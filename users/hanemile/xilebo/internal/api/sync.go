package api

import (
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = parsed
		}
	}

	// Get rooms the user is in
	rooms, err := s.store.ListUserRooms(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to list rooms")
		return
	}

	joinedRooms := make(map[string]any)
	for _, room := range rooms {
		msgs, err := s.store.GetMessagesSince(r.Context(), room.ID, since, 100)
		if err != nil {
			continue
		}

		events := make([]any, len(msgs))
		for i, m := range msgs {
			events[i] = map[string]any{
				"id":         m.ID,
				"type":       m.EventType,
				"room_id":    m.RoomID,
				"sender":     m.Sender,
				"timestamp":  m.ServerTS,
				"content":    m.Content,
			}
		}

		joinedRooms[room.ID] = map[string]any{
			"timeline": map[string]any{
				"events":  events,
				"limited": len(msgs) >= 100,
			},
		}
	}

	// Get to-device messages
	toDevice, err := s.store.GetToDeviceMessages(r.Context(), userID, 100)
	if err != nil {
		toDevice = nil
	}

	toDeviceEvents := make([]any, len(toDevice))
	for i, td := range toDevice {
		toDeviceEvents[i] = map[string]any{
			"type":    td.EventType,
			"sender":  td.Sender,
			"content": td.Content,
		}
	}

	// Clean up delivered to-device messages
	if len(toDevice) > 0 {
		s.store.DeleteToDeviceMessages(r.Context(), toDevice[len(toDevice)-1].ID, userID)
	}

	nextBatch := strconv.FormatInt(time.Now().UnixMilli(), 10)

	writeJSON(w, http.StatusOK, map[string]any{
		"next_batch": nextBatch,
		"rooms": map[string]any{
			"join": joinedRooms,
		},
		"to_device": toDeviceEvents,
	})
}
