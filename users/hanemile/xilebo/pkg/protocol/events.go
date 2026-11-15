package protocol

// Event types used in the xilebo wire protocol.
const (
	EventRoomMessage   = "m.room.message"
	EventRoomEncrypted = "m.room.encrypted"
	EventRoomMember    = "m.room.member"
	EventRoomCreate    = "m.room.create"
	EventRoomName      = "m.room.name"
	EventRoomTopic     = "m.room.topic"
	EventRoomAvatar    = "m.room.avatar"
	EventRoomPower     = "m.room.power_levels"
	EventTyping        = "m.typing"
	EventPresence      = "m.presence"
	EventCallInvite    = "m.call.invite"
	EventCallAnswer    = "m.call.answer"
	EventCallCandidates = "m.call.candidates"
	EventCallHangup    = "m.call.hangup"
	EventToDevice      = "m.to_device"
)

// Room types.
const (
	RoomTypeChat  = "chat"
	RoomTypeVideo = "video"
)

// Membership states.
const (
	MemberJoin   = "join"
	MemberLeave  = "leave"
	MemberInvite = "invite"
	MemberBan    = "ban"
)

// Message types within m.room.message content.
const (
	MsgTypeText  = "m.text"
	MsgTypeImage = "m.image"
	MsgTypeFile  = "m.file"
	MsgTypeVideo = "m.video"
	MsgTypeAudio = "m.audio"
)

// Event is the base envelope for all xilebo events.
type Event struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	RoomID     string      `json:"room_id,omitempty"`
	Sender     string      `json:"sender"`
	Timestamp  int64       `json:"timestamp"`
	Content    any `json:"content"`
	StateKey   string      `json:"state_key,omitempty"`
}

// MessageContent is the content of a m.room.message event.
type MessageContent struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
	URL     string `json:"url,omitempty"`
}

// EncryptedContent is the content of a m.room.encrypted event.
type EncryptedContent struct {
	Algorithm  string `json:"algorithm"`
	SenderKey  string `json:"sender_key"`
	SessionID  string `json:"session_id"`
	Ciphertext string `json:"ciphertext"`
}

// MemberContent is the content of a m.room.member event.
type MemberContent struct {
	Membership  string `json:"membership"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// CallInviteContent is the content of a m.call.invite event.
type CallInviteContent struct {
	CallID  string `json:"call_id"`
	Offer   RTCOffer `json:"offer"`
	Version int    `json:"version"`
}

// CallAnswerContent is the content of a m.call.answer event.
type CallAnswerContent struct {
	CallID string    `json:"call_id"`
	Answer RTCAnswer `json:"answer"`
}

// RTCOffer is an SDP offer.
type RTCOffer struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// RTCAnswer is an SDP answer.
type RTCAnswer struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// CallCandidatesContent carries ICE candidates.
type CallCandidatesContent struct {
	CallID     string         `json:"call_id"`
	Candidates []ICECandidate `json:"candidates"`
}

// ICECandidate is a single ICE candidate.
type ICECandidate struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdp_mid"`
	SDPMLineIndex int    `json:"sdp_mline_index"`
}
