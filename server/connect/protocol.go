package connect

import "encoding/json"

// Extension is the OpenSubsonic extension name this server advertises (getOpenSubsonicExtensions), and Version its
// version. Clients look for it before opening rest/connect, so they keep working against servers without it.
const (
	Extension = "sonusConnect"
	Version   = 1
)

// Message types. Client to server: hello, state, command, transfer. Server to client: welcome, devices, command, error.
const (
	typeHello    = "hello"
	typeState    = "state"
	typeCommand  = "command"
	typeTransfer = "transfer"
	typeWelcome  = "welcome"
	typeDevices  = "devices"
	typeError    = "error"
)

// Commands a device carries out. "load" and "stop" are what a transfer sends; any device may send any of them.
const (
	ActionPlay     = "play"
	ActionPause    = "pause"
	ActionToggle   = "toggle"
	ActionNext     = "next"
	ActionPrevious = "previous"
	ActionSeek     = "seek"
	ActionVolume   = "volume"
	ActionLoad     = "load"
	ActionStop     = "stop"
)

// Kinds of device, for the icon a client shows. Anything else is shown as "other".
const (
	KindDesktop = "desktop"
	KindWeb     = "web"
	KindPhone   = "phone"
	KindSpeaker = "speaker"
)

// envelope is every message's outer shape; the fields used depend on Type.
type envelope struct {
	Type string `json:"type"`

	// hello
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind,omitempty"`
	CanPlay *bool  `json:"canPlay,omitempty"`

	// state
	State *State `json:"state,omitempty"`
	Queue *Queue `json:"queue,omitempty"`

	// command, transfer
	Target  string   `json:"target,omitempty"`
	Command *Command `json:"command,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	Play    *bool    `json:"play,omitempty"`
}

// State is what a device is playing, as it reports it. PositionMs is the position at the moment the server received
// it, which the server records as At (its own clock, milliseconds since the Unix epoch): a client shows the position
// now as PositionMs + (serverNow - At) while Playing.
type State struct {
	TrackID    string       `json:"trackId,omitempty"`
	Title      string       `json:"title,omitempty"`
	Artist     string       `json:"artist,omitempty"`
	Album      string       `json:"album,omitempty"`
	CoverArt   string       `json:"coverArt,omitempty"`
	DurationMs int64        `json:"durationMs,omitempty"`
	PositionMs int64        `json:"positionMs"`
	Playing    bool         `json:"playing"`
	Volume     *float64     `json:"volume,omitempty"`
	Index      int          `json:"index"`
	Count      int          `json:"count"`
	Context    *PlayContext `json:"context,omitempty"`
	At         int64        `json:"at"`
}

// PlayContext is where the queue came from (an album, a playlist…), so another device can name it and play it the same way.
type PlayContext struct {
	Kind  string `json:"kind,omitempty"`
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
}

// Queue is a device's play queue: song ids and the playing one's index. Sent only when it changes; kept by the
// server for transfers, not broadcast.
type Queue struct {
	IDs   []string `json:"ids"`
	Index int      `json:"index"`
}

// Command is what a device is asked to do. Fields beyond Action depend on it: seek has PositionMs, volume has
// Volume, load has Queue (with its Index), PositionMs, Play and Context, and Source when a transfer sent it (the
// device the playback came from, which may not be the one that asked).
type Command struct {
	Action     string       `json:"action"`
	PositionMs int64        `json:"positionMs,omitempty"`
	Volume     *float64     `json:"volume,omitempty"`
	Queue      *Queue       `json:"queue,omitempty"`
	Play       *bool        `json:"play,omitempty"`
	Context    *PlayContext `json:"context,omitempty"`
	Source     string       `json:"source,omitempty"`
}

// Device is one connected player as the others see it.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Client      string `json:"client"`
	CanPlay     bool   `json:"canPlay"`
	ConnectedAt int64  `json:"connectedAt"`
	State       *State `json:"state,omitempty"`
}

type welcomeMessage struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	DeviceID   string `json:"deviceId"`
	ServerTime int64  `json:"serverTime"`
}

type devicesMessage struct {
	Type       string   `json:"type"`
	ServerTime int64    `json:"serverTime"`
	Devices    []Device `json:"devices"`
}

type commandMessage struct {
	Type    string   `json:"type"`
	From    string   `json:"from"`
	Command *Command `json:"command"`
}

type errorMessage struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func encode(v any) []byte {
	data, _ := json.Marshal(v) // only our own message types: they always encode
	return data
}
