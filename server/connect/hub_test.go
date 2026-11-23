package connect

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The hub behind a test server that does what the Subsonic middleware does: puts the user and the player in the
// request's context (here from ?user= and ?player=).
func serve(h *Hub) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ctx := r.Context()
		if u := q.Get("user"); u != "" {
			ctx = request.WithUser(ctx, model.User{ID: u, UserName: u})
		}
		if p := q.Get("player"); p != "" {
			ctx = request.WithPlayer(ctx, model.Player{ID: p, Name: "Player " + p, Client: "test client"})
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
}

type client struct {
	ws *websocket.Conn
	id string
}

func dial(srv *httptest.Server, user, player string) *client {
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"?user="+user+"&player="+player, nil)
	Expect(err).ToNot(HaveOccurred())
	c := &client{ws: ws}
	welcome := c.next(typeWelcome)
	Expect(welcome["version"]).To(BeNumerically("==", Version))
	c.id = welcome["deviceId"].(string)
	return c
}

func (c *client) write(v any) { Expect(c.ws.WriteJSON(v)).To(Succeed()) }

// next reads until a message of that type arrives (others are skipped).
func (c *client) next(kind string) map[string]any {
	for {
		Expect(c.ws.SetReadDeadline(time.Now().Add(2 * time.Second))).To(Succeed())
		_, data, err := c.ws.ReadMessage()
		Expect(err).ToNot(HaveOccurred(), "waiting for %q", kind)
		var msg map[string]any
		Expect(json.Unmarshal(data, &msg)).To(Succeed())
		if msg["type"] == kind {
			return msg
		}
	}
}

// devicesWhere reads device lists until one satisfies ok.
func (c *client) devicesWhere(ok func([]map[string]any) bool) []map[string]any {
	for {
		msg := c.next(typeDevices)
		var list []map[string]any
		for _, d := range msg["devices"].([]any) {
			list = append(list, d.(map[string]any))
		}
		if ok(list) {
			return list
		}
	}
}

func ids(list []map[string]any) []string {
	var out []string
	for _, d := range list {
		out = append(out, d["id"].(string))
	}
	return out
}

func (c *client) close() { _ = c.ws.Close() }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (k *clock) now() time.Time      { k.mu.Lock(); defer k.mu.Unlock(); return k.t }
func (k *clock) add(d time.Duration) { k.mu.Lock(); defer k.mu.Unlock(); k.t = k.t.Add(d) }

var _ = Describe("Hub", func() {
	var hub *Hub
	var srv *httptest.Server
	var k *clock

	BeforeEach(func() {
		hub = NewHub()
		k = &clock{t: time.UnixMilli(1_800_000_000_000)}
		hub.now = k.now
		srv = serve(hub)
	})
	AfterEach(func() { srv.Close() })

	It("refuses a request that is not a WebSocket, or has no player", func() {
		res, err := http.Get(srv.URL + "?user=u1&player=p1")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
		res, err = http.Get(srv.URL + "?user=u1")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("shows each of a user's players the others, and keeps users apart", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		b := dial(srv, "u1", "p2")
		defer b.close()
		other := dial(srv, "u2", "p3")
		defer other.close()

		Expect(ids(a.devicesWhere(func(l []map[string]any) bool { return len(l) == 2 }))).To(ConsistOf("p1", "p2"))
		Expect(ids(other.devicesWhere(func(l []map[string]any) bool { return len(l) == 1 }))).To(ConsistOf("p3"))

		b.close()
		Expect(ids(a.devicesWhere(func(l []map[string]any) bool { return len(l) == 1 }))).To(ConsistOf("p1"))
	})

	It("names a player as it says, with a known kind or 'other'", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		a.write(map[string]any{"type": "hello", "name": "书房电脑", "kind": "desktop", "canPlay": true})
		list := a.devicesWhere(func(l []map[string]any) bool { return l[0]["name"] == "书房电脑" })
		Expect(list[0]["kind"]).To(Equal("desktop"))
		a.write(map[string]any{"type": "hello", "kind": "toaster"})
		list = a.devicesWhere(func(l []map[string]any) bool { return l[0]["kind"] != "desktop" })
		Expect(list[0]["kind"]).To(Equal("other"))
		Expect(list[0]["name"]).To(Equal("书房电脑"))
	})

	It("shares a player's state stamped with the server's time, but not its queue", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		b := dial(srv, "u1", "p2")
		defer b.close()
		a.write(map[string]any{"type": "state", "state": map[string]any{"trackId": "s2", "title": "Song", "positionMs": 1500, "playing": true, "index": 1, "count": 3},
			"queue": map[string]any{"ids": []string{"s1", "s2", "s3"}, "index": 1}})
		list := b.devicesWhere(func(l []map[string]any) bool {
			for _, d := range l {
				if d["id"] == "p1" && d["state"] != nil {
					return true
				}
			}
			return false
		})
		for _, d := range list {
			if d["id"] == "p1" {
				state := d["state"].(map[string]any)
				Expect(state["trackId"]).To(Equal("s2"))
				Expect(state["at"]).To(BeNumerically("==", k.now().UnixMilli()))
				Expect(state).ToNot(HaveKey("ids"))
			}
		}
	})

	It("passes a command to the player it names, and says when that player is gone", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		b := dial(srv, "u1", "p2")
		defer b.close()
		a.write(map[string]any{"type": "command", "target": "p2", "command": map[string]any{"action": "seek", "positionMs": 42000}})
		cmd := b.next(typeCommand)
		Expect(cmd["from"]).To(Equal("p1"))
		Expect(cmd["command"]).To(HaveKeyWithValue("action", "seek"))
		Expect(cmd["command"]).To(HaveKeyWithValue("positionMs", BeNumerically("==", 42000)))

		a.write(map[string]any{"type": "command", "target": "nobody", "command": map[string]any{"action": "pause"}})
		Expect(a.next(typeError)["code"]).To(Equal("offline"))
	})

	It("does not let a command reach another user's player", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		other := dial(srv, "u2", "p9")
		defer other.close()
		a.write(map[string]any{"type": "command", "target": "p9", "command": map[string]any{"action": "pause"}})
		Expect(a.next(typeError)["code"]).To(Equal("offline"))
	})

	It("transfers playback: the target loads the queue where the source is now, the source stops", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		b := dial(srv, "u1", "p2")
		defer b.close()
		a.write(map[string]any{"type": "state", "state": map[string]any{"trackId": "s2", "positionMs": 10_000, "playing": true, "index": 1, "count": 3,
			"context": map[string]any{"kind": "album", "id": "al1", "label": "Orbit"}},
			"queue": map[string]any{"ids": []string{"s1", "s2", "s3"}, "index": 1}})
		b.devicesWhere(func(l []map[string]any) bool {
			for _, d := range l {
				if d["id"] == "p1" && d["state"] != nil {
					return true
				}
			}
			return false
		})
		k.add(5 * time.Second) // it went on playing

		b.write(map[string]any{"type": "transfer", "from": "p1", "to": "p2"})
		load := b.next(typeCommand)["command"].(map[string]any)
		Expect(load["action"]).To(Equal(ActionLoad))
		Expect(load["positionMs"]).To(BeNumerically("==", 15_000))
		Expect(load["play"]).To(BeTrue())
		Expect(load["queue"]).To(HaveKeyWithValue("ids", ConsistOf("s1", "s2", "s3")))
		Expect(load["queue"]).To(HaveKeyWithValue("index", BeNumerically("==", 1)))
		Expect(load["context"]).To(HaveKeyWithValue("id", "al1"))
		Expect(load["source"]).To(Equal("p1")) // where it came from, though p2 asked
		Expect(a.next(typeCommand)["command"]).To(HaveKeyWithValue("action", ActionStop))
	})

	It("says when there is nothing to transfer", func() {
		a := dial(srv, "u1", "p1")
		defer a.close()
		b := dial(srv, "u1", "p2")
		defer b.close()
		b.write(map[string]any{"type": "transfer", "from": "p1", "to": "p2"})
		Expect(b.next(typeError)["code"]).To(Equal("nothing"))
	})

	It("lets a reconnecting player replace its old connection and keep its state", func() {
		a := dial(srv, "u1", "p1")
		a.write(map[string]any{"type": "state", "state": map[string]any{"trackId": "s1", "positionMs": 0, "playing": false}})
		a.devicesWhere(func(l []map[string]any) bool { return len(l) == 1 && l[0]["state"] != nil })

		again := dial(srv, "u1", "p1")
		defer again.close()
		list := again.devicesWhere(func(l []map[string]any) bool { return len(l) == 1 })
		Expect(list[0]["state"]).To(HaveKeyWithValue("trackId", "s1"))
		Eventually(func() error { _, _, err := a.ws.ReadMessage(); return err }).Should(HaveOccurred()) // the old one was closed
		a.close()
		Consistently(func() int { return len(hub.Devices("u1")) }, 300*time.Millisecond).Should(Equal(1))
	})
})
