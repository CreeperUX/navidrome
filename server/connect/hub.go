// Package connect lets a user's players see and control each other, as Spotify Connect does: on a WebSocket at
// rest/connect (authenticated like every Subsonic call), each connected player says what it plays, sees what the
// others play, and can tell any of them to play, pause, seek, skip, change volume, or take over the playback of
// another (a transfer). State lives in memory only: nothing is stored, so a server without this package runs on the
// same database. The protocol is described in README.md.
package connect

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils/singleton"
)

// MaxDevices is how many players one user may have connected at once; the oldest goes when another arrives.
const MaxDevices = 32

// GetHub is the server's hub, mounted on the Subsonic API (server/subsonic).
func GetHub() *Hub {
	return singleton.GetInstance(func() *Hub { return NewHub() })
}

// Hub keeps every user's connected players and passes messages between them. Each user has a room of their own:
// nothing crosses from one user to another.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*room // by user id
	now   func() time.Time
}

type room struct {
	devices map[string]*device // by player id
}

type device struct {
	Device
	queue *Queue
	conn  *conn
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]*room{}, now: time.Now}
}

func (h *Hub) millis() int64 { return h.now().UnixMilli() }

var upgrader = websocket.Upgrader{
	// Native clients aren't browsers; browsers send their credentials in the query, so a page on another origin
	// learns nothing it didn't already have.
	CheckOrigin: func(*http.Request) bool { return true },
}

// ServeHTTP turns an authenticated Subsonic request (the user and the player in its context) into a connection.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, okUser := request.UserFrom(ctx)
	player, okPlayer := request.PlayerFrom(ctx)
	if !okUser || !okPlayer || player.ID == "" {
		http.Error(w, "connect: no player for this request", http.StatusUnauthorized)
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "connect: a WebSocket upgrade is expected", http.StatusBadRequest)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn(ctx, "Connect: WebSocket upgrade failed", "user", user.UserName, err)
		return
	}
	c := newConn(ws)
	go c.writeLoop()

	name := cmp.Or(player.Name, player.Client)
	h.join(user.ID, &device{Device: Device{ID: player.ID, Name: name, Kind: "other", Client: player.Client,
		CanPlay: true, ConnectedAt: h.millis()}, conn: c})
	log.Debug(ctx, "Connect: player connected", "user", user.UserName, "player", name, "id", player.ID)
	c.send(encode(welcomeMessage{Type: typeWelcome, Version: Version, DeviceID: player.ID, ServerTime: h.millis()}))
	h.broadcast(user.ID)

	c.readLoop(ctx, func(msg envelope) { h.handle(ctx, user.ID, player.ID, msg) })
	if h.leave(user.ID, player.ID, c) {
		log.Debug(ctx, "Connect: player disconnected", "user", user.UserName, "player", name)
		h.broadcast(user.ID)
	}
}

// join adds a device, replacing an older connection of the same player (a reconnect) and making room if needed.
// Connections it ends are closed after the lock is released (closing writes to the network).
func (h *Hub) join(userID string, d *device) {
	var ended []*conn
	h.mu.Lock()
	rm := h.rooms[userID]
	if rm == nil {
		rm = &room{devices: map[string]*device{}}
		h.rooms[userID] = rm
	}
	if old := rm.devices[d.ID]; old != nil {
		d.State, d.queue = old.State, old.queue // the same player, reconnected: what it played still holds
		d.Name, d.Kind, d.CanPlay = old.Name, old.Kind, old.CanPlay
		ended = append(ended, old.conn)
	} else if len(rm.devices) >= MaxDevices {
		oldest := slices.MinFunc(slices.Collect(maps.Values(rm.devices)), func(a, b *device) int { return cmp.Compare(a.ConnectedAt, b.ConnectedAt) })
		delete(rm.devices, oldest.ID)
		ended = append(ended, oldest.conn)
	}
	rm.devices[d.ID] = d
	h.mu.Unlock()
	for _, c := range ended {
		c.close(4000, "replaced by a newer connection")
	}
}

// leave removes the device if c is still its connection (not one that replaced it); true when it did.
func (h *Hub) leave(userID, deviceID string, c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	rm := h.rooms[userID]
	if rm == nil || rm.devices[deviceID] == nil || rm.devices[deviceID].conn != c {
		return false
	}
	delete(rm.devices, deviceID)
	if len(rm.devices) == 0 {
		delete(h.rooms, userID)
	}
	return true
}

func (h *Hub) handle(ctx context.Context, userID, deviceID string, msg envelope) {
	switch msg.Type {
	case typeHello:
		h.update(userID, deviceID, func(d *device) {
			if msg.Name != "" {
				d.Name = truncate(msg.Name, 80)
			}
			if msg.Kind != "" {
				d.Kind = kind(msg.Kind)
			}
			if msg.CanPlay != nil {
				d.CanPlay = *msg.CanPlay
			}
		})
		h.broadcast(userID)
	case typeState:
		if msg.State == nil {
			return
		}
		h.update(userID, deviceID, func(d *device) {
			s := *msg.State
			s.At = h.millis()
			d.State = &s
			if msg.Queue != nil {
				d.queue = msg.Queue
			}
		})
		h.broadcast(userID)
	case typeCommand:
		if msg.Command == nil || msg.Target == "" {
			return
		}
		if !h.sendTo(userID, msg.Target, encode(commandMessage{Type: typeCommand, From: deviceID, Command: msg.Command})) {
			h.reply(userID, deviceID, "offline", "该设备已离线。")
		}
	case typeTransfer:
		h.transfer(ctx, userID, deviceID, msg)
	default:
		log.Debug(ctx, "Connect: unknown message", "type", msg.Type)
	}
}

// transfer moves a device's playback to another: the target loads the source's queue at the source's position (now),
// playing if the source was (or as asked), and the source stops.
func (h *Hub) transfer(ctx context.Context, userID, requester string, msg envelope) {
	from := cmp.Or(msg.From, requester)
	if msg.To == "" || msg.To == from {
		return
	}
	h.mu.Lock()
	rm := h.rooms[userID]
	var src, dst *device
	if rm != nil {
		src, dst = rm.devices[from], rm.devices[msg.To]
	}
	var load *Command
	if src != nil && dst != nil && src.State != nil && src.queue != nil && len(src.queue.IDs) > 0 {
		position := src.State.PositionMs
		if src.State.Playing {
			position += h.millis() - src.State.At
		}
		play := src.State.Playing
		if msg.Play != nil {
			play = *msg.Play
		}
		queue := *src.queue
		load = &Command{Action: ActionLoad, Queue: &queue, PositionMs: position, Play: &play, Context: src.State.Context, Source: from}
	}
	h.mu.Unlock()

	switch {
	case dst == nil:
		h.reply(userID, requester, "offline", "目标设备已离线。")
	case load == nil:
		h.reply(userID, requester, "nothing", "这台设备没有可以转移的播放内容。")
	default:
		log.Debug(ctx, "Connect: transfer", "from", from, "to", msg.To, "songs", len(load.Queue.IDs))
		h.sendTo(userID, msg.To, encode(commandMessage{Type: typeCommand, From: requester, Command: load}))
		h.sendTo(userID, from, encode(commandMessage{Type: typeCommand, From: requester, Command: &Command{Action: ActionStop}}))
	}
}

func (h *Hub) update(userID, deviceID string, change func(*device)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if rm := h.rooms[userID]; rm != nil {
		if d := rm.devices[deviceID]; d != nil {
			change(d)
		}
	}
}

// broadcast sends every device of the user the list of all of them (a handful: the whole list each time keeps
// clients simple).
func (h *Hub) broadcast(userID string) {
	h.mu.Lock()
	rm := h.rooms[userID]
	if rm == nil {
		h.mu.Unlock()
		return
	}
	list := make([]Device, 0, len(rm.devices))
	conns := make([]*conn, 0, len(rm.devices))
	for _, d := range rm.devices {
		list = append(list, d.Device)
		conns = append(conns, d.conn)
	}
	h.mu.Unlock()
	slices.SortFunc(list, func(a, b Device) int {
		return cmp.Or(cmp.Compare(a.ConnectedAt, b.ConnectedAt), cmp.Compare(a.ID, b.ID))
	})
	data := encode(devicesMessage{Type: typeDevices, ServerTime: h.millis(), Devices: list})
	for _, c := range conns {
		c.send(data)
	}
}

func (h *Hub) sendTo(userID, deviceID string, data []byte) bool {
	h.mu.Lock()
	var c *conn
	if rm := h.rooms[userID]; rm != nil && rm.devices[deviceID] != nil {
		c = rm.devices[deviceID].conn
	}
	h.mu.Unlock()
	if c == nil {
		return false
	}
	c.send(data)
	return true
}

func (h *Hub) reply(userID, deviceID, code, message string) {
	h.sendTo(userID, deviceID, encode(errorMessage{Type: typeError, Code: code, Message: message}))
}

// Devices lists a user's connected players (for tests and diagnostics).
func (h *Hub) Devices(userID string) []Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	var list []Device
	if rm := h.rooms[userID]; rm != nil {
		for _, d := range rm.devices {
			list = append(list, d.Device)
		}
	}
	return list
}

func kind(k string) string {
	switch k {
	case KindDesktop, KindWeb, KindPhone, KindSpeaker:
		return k
	}
	return "other"
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// decode reads one message; false when it isn't one.
func decode(data []byte) (envelope, bool) {
	var msg envelope
	return msg, json.Unmarshal(data, &msg) == nil && msg.Type != ""
}
