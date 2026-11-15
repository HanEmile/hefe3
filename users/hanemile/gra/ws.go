package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Client represents a connected frontend
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub manages all websocket connections
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				client.send <- message
			}
		}
	}
}

// Global Hub instance
var wshub = newHub()

type WsMessage struct {
	Msgtype string `json:"type"`
	Data    any    `json:"data"`
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: wshub, conn: conn, send: make(chan []byte, 256)}
	wshub.register <- client

	// Read loop (for real-time adjustments)
	go func() {
		defer func() {
			wshub.unregister <- client
			conn.Close()
		}()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
            // Handle incoming adjustments (e.g., {"action": "set_threads", "job_id": "...", "value": 20})
			// handleWsAction(message)
			log.Println("ws: ", string(message))
		}
	}()

	// Write loop
	go func() {
		// for message := range client.send {
		// 	conn.WriteMessage(websocket.TextMessage, message)
		// }

		for {
			select {
			case message := <-client.send:
				conn.WriteMessage(websocket.TextMessage, message)
			case <-time.After(time.Second):
				now := time.Now().String()
				msg := WsMessage{
					Msgtype: "time",
					Data:    now,
				}
				jsonmsg, err := json.Marshal(msg)
				if err != nil {
					log.Println(err)
				}
				conn.WriteMessage(websocket.TextMessage, []byte(jsonmsg))
			}
		}
	}()
}
