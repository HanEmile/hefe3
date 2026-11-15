package storage

import "context"

// Store is the data access interface for the xilebo homeserver.
// Both SQLite and PostgreSQL implementations satisfy this interface.
type Store interface {
	// Users
	CreateUser(ctx context.Context, p CreateUserParams) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	GetUserByHandle(ctx context.Context, handle string) (User, error)
	UpdateProfile(ctx context.Context, id string, displayName, avatarURL string) error
	UpdateInstancePerms(ctx context.Context, id string, perms uint64) error
	SetUserTags(ctx context.Context, userID string, tags []string) error
	GetUserTags(ctx context.Context, userID string) ([]string, error)
	ListUsers(ctx context.Context, limit, offset int) ([]User, error)

	// Rooms
	CreateRoom(ctx context.Context, p CreateRoomParams) (Room, error)
	GetRoom(ctx context.Context, id string) (Room, error)
	ListUserRooms(ctx context.Context, userID string) ([]Room, error)
	AddMember(ctx context.Context, roomID, userID string, powerLevel int, joinedAt int64) error
	RemoveMember(ctx context.Context, roomID, userID string) error
	GetMembers(ctx context.Context, roomID string) ([]RoomMember, error)
	GetMember(ctx context.Context, roomID, userID string) (RoomMember, error)
	SetPowerLevel(ctx context.Context, roomID, userID string, level int) error
	GetRoomState(ctx context.Context, roomID string) ([]RoomState, error)
	SetRoomState(ctx context.Context, p SetRoomStateParams) error

	// Messages
	InsertMessage(ctx context.Context, p InsertMessageParams) error
	GetMessages(ctx context.Context, roomID string, before int64, limit int) ([]Message, error)
	GetMessagesSince(ctx context.Context, roomID string, since int64, limit int) ([]Message, error)
	GetMessageByID(ctx context.Context, id string) (Message, error)
	DeleteMessage(ctx context.Context, id string) error

	// Crypto keys
	StorePreKey(ctx context.Context, p StorePreKeyParams) error
	ClaimPreKey(ctx context.Context, userID string) (PreKey, error)
	GetSignedPreKey(ctx context.Context, userID string) (PreKey, error)
	CountUnclaimedPreKeys(ctx context.Context, userID string) (int64, error)

	// Sessions
	StoreSession(ctx context.Context, p StoreSessionParams) error
	GetSession(ctx context.Context, localUserID, remoteUserID string) (Session, error)
	DeleteSession(ctx context.Context, id string) error

	// Permissions
	GetInstancePerms(ctx context.Context, userID string) (uint64, error)
	GetPowerLevel(ctx context.Context, roomID, userID string) (int, error)

	// To-device messages
	StoreToDeviceMessage(ctx context.Context, p ToDeviceParams) error
	GetToDeviceMessages(ctx context.Context, recipient string, limit int) ([]ToDeviceMessage, error)
	DeleteToDeviceMessages(ctx context.Context, upToID int64, recipient string) error

	// Federation
	EnqueueFederationEvent(ctx context.Context, p EnqueueFedEventParams) error
	GetPendingFederationEvents(ctx context.Context, server string, before int64, limit int) ([]FederationQueueItem, error)
	MarkFederationEventSent(ctx context.Context, id string, sentAt int64) error
	IncrementFederationAttempt(ctx context.Context, id string, nextRetry int64) error
	StoreFederationServer(ctx context.Context, p FederationServerParams) error
	GetFederationServer(ctx context.Context, hostname string) (FederationServer, error)

	// Media
	StoreMedia(ctx context.Context, p StoreMediaParams) error
	GetMedia(ctx context.Context, id string) (MediaMeta, error)

	// Lifecycle
	Close() error
}

// Domain models — decoupled from sqlc-generated types.

type User struct {
	ID            string
	Handle        string
	DisplayName   string
	AvatarURL     string
	IdentityKey   []byte
	PasswordHash  string
	InstancePerms uint64
	CreatedAt     int64
	UpdatedAt     int64
}

type Room struct {
	ID        string
	Name      string
	Topic     string
	AvatarURL string
	IsDirect  bool
	RoomType  string
	CreatedBy string
	CreatedAt int64
}

type RoomMember struct {
	RoomID     string
	UserID     string
	PowerLevel int
	JoinedAt   int64
}

type RoomState struct {
	RoomID    string
	EventType string
	StateKey  string
	Content   string
	Sender    string
	Timestamp int64
}

type Message struct {
	ID          string
	RoomID      string
	Sender      string
	EventType   string
	Content     string
	OriginTS    int64
	ServerTS    int64
	VectorClock string
}

type PreKey struct {
	ID        int64
	UserID    string
	KeyID     int64
	PublicKey []byte
	IsSigned  bool
	Signature []byte
	Claimed   bool
	CreatedAt int64
}

type Session struct {
	ID           string
	LocalUserID  string
	RemoteUserID string
	SessionData  []byte
	UpdatedAt    int64
}

type FederationQueueItem struct {
	ID           string
	TargetServer string
	EventData    string
	CreatedAt    int64
	Attempts     int
	NextRetry    int64
	SentAt       *int64
}

type FederationServer struct {
	Hostname   string
	PublicKey  []byte
	VerifiedAt int64
	LastSeen   int64
}

type MediaMeta struct {
	ID          string
	UploaderID  string
	Filename    string
	ContentType string
	SizeBytes   int64
	StoragePath string
	CreatedAt   int64
}

type ToDeviceMessage struct {
	ID        int64
	Sender    string
	Recipient string
	EventType string
	Content   string
	CreatedAt int64
}

// Parameter structs for create/insert operations.

type CreateUserParams struct {
	ID            string
	Handle        string
	DisplayName   string
	AvatarURL     string
	IdentityKey   []byte
	PasswordHash  string
	InstancePerms uint64
	CreatedAt     int64
	UpdatedAt     int64
}

type CreateRoomParams struct {
	ID        string
	Name      string
	Topic     string
	AvatarURL string
	IsDirect  bool
	RoomType  string
	CreatedBy string
	CreatedAt int64
}

type SetRoomStateParams struct {
	RoomID    string
	EventType string
	StateKey  string
	Content   string
	Sender    string
	Timestamp int64
}

type InsertMessageParams struct {
	ID          string
	RoomID      string
	Sender      string
	EventType   string
	Content     string
	OriginTS    int64
	ServerTS    int64
	VectorClock string
}

type StorePreKeyParams struct {
	UserID    string
	KeyID     int64
	PublicKey []byte
	IsSigned  bool
	Signature []byte
	CreatedAt int64
}

type StoreSessionParams struct {
	ID           string
	LocalUserID  string
	RemoteUserID string
	SessionData  []byte
	UpdatedAt    int64
}

type ToDeviceParams struct {
	Sender    string
	Recipient string
	EventType string
	Content   string
	CreatedAt int64
}

type EnqueueFedEventParams struct {
	ID           string
	TargetServer string
	EventData    string
	CreatedAt    int64
	NextRetry    int64
}

type FederationServerParams struct {
	Hostname   string
	PublicKey  []byte
	VerifiedAt int64
	LastSeen   int64
}

type StoreMediaParams struct {
	ID          string
	UploaderID  string
	Filename    string
	ContentType string
	SizeBytes   int64
	StoragePath string
	CreatedAt   int64
}
