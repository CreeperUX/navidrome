package connect

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/navidrome/navidrome/log"
)

const (
	// pingInterval: the server pings every connection this often; a client's WebSocket answers by itself.
	pingInterval = 30 * time.Second
	// readTimeout: a connection that sends nothing (not even a pong) for this long is gone.
	readTimeout  = 75 * time.Second
	writeTimeout = 10 * time.Second
	// maxMessage bounds what a client may send: a state with a queue of a few thousand songs fits easily.
	maxMessage = 2 << 20
	// sendBuffer: messages waiting for a slow connection. One that falls this far behind is closed.
	sendBuffer = 64
)

// conn is one player's WebSocket: reads on the request's goroutine, writes (messages and pings) on its own.
type conn struct {
	ws       *websocket.Conn
	out      chan []byte
	done     chan struct{}
	closeErr sync.Once
}

func newConn(ws *websocket.Conn) *conn {
	return &conn{ws: ws, out: make(chan []byte, sendBuffer), done: make(chan struct{})}
}

// send queues a message without waiting; a connection whose queue is full is closed (it isn't reading).
func (c *conn) send(data []byte) {
	select {
	case <-c.done:
	case c.out <- data:
	default:
		c.close(websocket.CloseTryAgainLater, "too slow")
	}
}

// close ends the connection once, telling the client why.
func (c *conn) close(code int, reason string) {
	c.closeErr.Do(func() {
		close(c.done)
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(writeTimeout))
		_ = c.ws.Close()
	})
}

func (c *conn) writeLoop() {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.done:
			return
		case data := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				c.close(websocket.CloseGoingAway, "write failed")
				return
			}
		case <-ping.C:
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				c.close(websocket.CloseGoingAway, "ping failed")
				return
			}
		}
	}
}

// readLoop hands every message to handle until the connection ends.
func (c *conn) readLoop(ctx context.Context, handle func(envelope)) {
	defer c.close(websocket.CloseNormalClosure, "")
	c.ws.SetReadLimit(maxMessage)
	_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))
	c.ws.SetPongHandler(func(string) error { return c.ws.SetReadDeadline(time.Now().Add(readTimeout)) })
	for {
		kind, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				log.Debug(ctx, "Connect: connection ended", err)
			}
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))
		if kind != websocket.TextMessage {
			continue
		}
		if msg, ok := decode(data); ok {
			handle(msg)
		}
	}
}
