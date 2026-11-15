package api

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/hanemile/xilebo/internal/api/ws"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // CORS handled by middleware
	},
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("ws upgrade", "error", err)
		return
	}

	c := ws.NewConn(conn, userID, s.hub, s.logger)
	s.hub.Register(c)

	// Subscribe to all rooms the user is in
	rooms, err := s.store.ListUserRooms(r.Context(), userID)
	if err == nil {
		for _, room := range rooms {
			s.hub.JoinRoom(room.ID, c)
		}
	}

	go c.WritePump()
	go c.ReadPump()
}
