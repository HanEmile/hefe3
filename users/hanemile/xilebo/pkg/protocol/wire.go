package protocol

import "encoding/json"

// RawEvent is an Event with unparsed Content for wire-level handling.
type RawEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	RoomID    string          `json:"room_id,omitempty"`
	Sender    string          `json:"sender"`
	Timestamp int64           `json:"timestamp"`
	Content   json.RawMessage `json:"content"`
	StateKey  string          `json:"state_key,omitempty"`
}

// SyncResponse is returned by the /sync endpoint.
type SyncResponse struct {
	NextBatch string         `json:"next_batch"`
	Rooms     SyncRooms      `json:"rooms"`
	ToDevice  []RawEvent     `json:"to_device,omitempty"`
	Presence  []RawEvent     `json:"presence,omitempty"`
}

// SyncRooms groups room updates by category.
type SyncRooms struct {
	Join   map[string]JoinedRoom `json:"join,omitempty"`
	Invite map[string]InvitedRoom `json:"invite,omitempty"`
	Leave  map[string]LeftRoom    `json:"leave,omitempty"`
}

// JoinedRoom contains updates for a room the user is in.
type JoinedRoom struct {
	Timeline   Timeline   `json:"timeline"`
	State      StateEvents `json:"state"`
	UnreadCount int        `json:"unread_count"`
}

// InvitedRoom contains the invite state.
type InvitedRoom struct {
	InviteState StateEvents `json:"invite_state"`
}

// LeftRoom contains events since the user left.
type LeftRoom struct {
	Timeline Timeline `json:"timeline"`
}

// Timeline is an ordered list of events.
type Timeline struct {
	Events    []RawEvent `json:"events"`
	Limited   bool       `json:"limited"`
	PrevBatch string     `json:"prev_batch,omitempty"`
}

// StateEvents is a list of state events.
type StateEvents struct {
	Events []RawEvent `json:"events"`
}

// ErrorResponse is returned on API errors.
type ErrorResponse struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

// PreKeyBundle is the public key material for establishing an encrypted session.
type PreKeyBundle struct {
	UserID        string `json:"user_id"`
	IdentityKey   []byte `json:"identity_key"`
	SignedPreKey   PublicPreKey `json:"signed_pre_key"`
	OneTimePreKey *PublicPreKey `json:"one_time_pre_key,omitempty"`
}

// PublicPreKey is a pre-key as seen by other users.
type PublicPreKey struct {
	KeyID     int    `json:"key_id"`
	PublicKey []byte `json:"public_key"`
	Signature []byte `json:"signature,omitempty"`
}

// Profile is the public profile of a user.
type Profile struct {
	Handle      string   `json:"handle"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url"`
	IdentityKey []byte   `json:"identity_key"`
	Tags        []string `json:"tags"`
}
