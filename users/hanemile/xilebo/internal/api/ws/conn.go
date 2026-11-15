package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 64 * 1024 // 64KB
)

// Conn wraps a WebSocket connection with send buffering.
type Conn struct {
	ws     *websocket.Conn
	userID string
	send   chan []byte
	hub    *Hub
	logger *slog.Logger
	once   sync.Once
}

// NewConn creates a new Conn.
func NewConn(ws *websocket.Conn, userID string, hub *Hub, logger *slog.Logger) *Conn {
	return &Conn{
		ws:     ws,
		userID: userID,
		send:   make(chan []byte, 256),
		hub:    hub,
		logger: logger,
	}
}

// UserID returns the authenticated user ID.
func (c *Conn) UserID() string {
	return c.userID
}

// Send marshals and queues a message for sending.
func (c *Conn) Send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.send <- data:
		return nil
	default:
		// Buffer full, drop message
		return nil
	}
}

// ReadPump reads messages from the WebSocket. Runs in its own goroutine.
func (c *Conn) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.ws.Close()
	}()

	c.ws.SetReadLimit(maxMsgSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Debug("ws read error", "error", err)
			}
			return
		}
		// Client messages are handled here in the future (typing indicators, etc.)
	}
}

// WritePump writes messages to the WebSocket. Runs in its own goroutine.
func (c *Conn) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	for {
		select {
		case data, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close stops the connection.
func (c *Conn) Close() {
	c.once.Do(func() {
		close(c.send)
	})
}
