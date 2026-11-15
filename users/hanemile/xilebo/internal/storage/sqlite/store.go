package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hanemile/xilebo/internal/storage"
	db "github.com/hanemile/xilebo/internal/storage/sqlite/queries"
)

// Compile-time check that Store implements storage.Store.
var _ storage.Store = (*Store)(nil)

// Store implements storage.Store using SQLite.
type Store struct {
	conn *sql.DB
	q    *db.Queries
}

// New opens a SQLite database and returns a Store.
func New(dsn string) (*Store, error) {
	conn, err := sql.Open("sqlite", dsn+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1) // SQLite serializes writes
	conn.SetConnMaxLifetime(0)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &Store{conn: conn, q: db.New(conn)}, nil
}

func (s *Store) Close() error {
	return s.conn.Close()
}

// DB returns the underlying sql.DB for migrations.
func (s *Store) DB() *sql.DB {
	return s.conn
}

// --- Users ---

func (s *Store) CreateUser(ctx context.Context, p storage.CreateUserParams) (storage.User, error) {
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID:            p.ID,
		Handle:        p.Handle,
		DisplayName:   p.DisplayName,
		AvatarUrl:     p.AvatarURL,
		IdentityKey:   p.IdentityKey,
		PasswordHash:  p.PasswordHash,
		InstancePerms: int64(p.InstancePerms),
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	})
	if err != nil {
		return storage.User{}, err
	}
	return convUser(u), nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (storage.User, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return storage.User{}, err
	}
	return convUser(u), nil
}

func (s *Store) GetUserByHandle(ctx context.Context, handle string) (storage.User, error) {
	u, err := s.q.GetUserByHandle(ctx, handle)
	if err != nil {
		return storage.User{}, err
	}
	return convUser(u), nil
}

func (s *Store) UpdateProfile(ctx context.Context, id string, displayName, avatarURL string) error {
	return s.q.UpdateProfile(ctx, db.UpdateProfileParams{
		DisplayName: displayName,
		AvatarUrl:   avatarURL,
		UpdatedAt:   time.Now().UnixMilli(),
		ID:          id,
	})
}

func (s *Store) UpdateInstancePerms(ctx context.Context, id string, perms uint64) error {
	return s.q.UpdateInstancePerms(ctx, db.UpdateInstancePermsParams{
		InstancePerms: int64(perms),
		UpdatedAt:     time.Now().UnixMilli(),
		ID:            id,
	})
}

func (s *Store) SetUserTags(ctx context.Context, userID string, tags []string) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	if err := qtx.SetUserTags(ctx, userID); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := qtx.InsertUserTag(ctx, db.InsertUserTagParams{UserID: userID, Tag: tag}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetUserTags(ctx context.Context, userID string) ([]string, error) {
	return s.q.GetUserTags(ctx, userID)
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]storage.User, error) {
	rows, err := s.q.ListUsers(ctx, db.ListUsersParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]storage.User, len(rows))
	for i, r := range rows {
		out[i] = convUser(r)
	}
	return out, nil
}

// --- Rooms ---

func (s *Store) CreateRoom(ctx context.Context, p storage.CreateRoomParams) (storage.Room, error) {
	isDirect := int64(0)
	if p.IsDirect {
		isDirect = 1
	}
	r, err := s.q.CreateRoom(ctx, db.CreateRoomParams{
		ID:        p.ID,
		Name:      p.Name,
		Topic:     p.Topic,
		AvatarUrl: p.AvatarURL,
		IsDirect:  isDirect,
		RoomType:  p.RoomType,
		CreatedBy: p.CreatedBy,
		CreatedAt: p.CreatedAt,
	})
	if err != nil {
		return storage.Room{}, err
	}
	return convRoom(r), nil
}

func (s *Store) GetRoom(ctx context.Context, id string) (storage.Room, error) {
	r, err := s.q.GetRoom(ctx, id)
	if err != nil {
		return storage.Room{}, err
	}
	return convRoom(r), nil
}

func (s *Store) ListUserRooms(ctx context.Context, userID string) ([]storage.Room, error) {
	rows, err := s.q.ListUserRooms(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Room, len(rows))
	for i, r := range rows {
		out[i] = convRoom(r)
	}
	return out, nil
}

func (s *Store) AddMember(ctx context.Context, roomID, userID string, powerLevel int, joinedAt int64) error {
	return s.q.AddMember(ctx, db.AddMemberParams{
		RoomID:     roomID,
		UserID:     userID,
		PowerLevel: int64(powerLevel),
		JoinedAt:   joinedAt,
	})
}

func (s *Store) RemoveMember(ctx context.Context, roomID, userID string) error {
	return s.q.RemoveMember(ctx, db.RemoveMemberParams{RoomID: roomID, UserID: userID})
}

func (s *Store) GetMembers(ctx context.Context, roomID string) ([]storage.RoomMember, error) {
	rows, err := s.q.GetMembers(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.RoomMember, len(rows))
	for i, r := range rows {
		out[i] = storage.RoomMember{
			RoomID:     r.RoomID,
			UserID:     r.UserID,
			PowerLevel: int(r.PowerLevel),
			JoinedAt:   r.JoinedAt,
		}
	}
	return out, nil
}

func (s *Store) GetMember(ctx context.Context, roomID, userID string) (storage.RoomMember, error) {
	r, err := s.q.GetMember(ctx, db.GetMemberParams{RoomID: roomID, UserID: userID})
	if err != nil {
		return storage.RoomMember{}, err
	}
	return storage.RoomMember{
		RoomID:     r.RoomID,
		UserID:     r.UserID,
		PowerLevel: int(r.PowerLevel),
		JoinedAt:   r.JoinedAt,
	}, nil
}

func (s *Store) SetPowerLevel(ctx context.Context, roomID, userID string, level int) error {
	return s.q.SetPowerLevel(ctx, db.SetPowerLevelParams{
		PowerLevel: int64(level),
		RoomID:     roomID,
		UserID:     userID,
	})
}

func (s *Store) GetRoomState(ctx context.Context, roomID string) ([]storage.RoomState, error) {
	rows, err := s.q.GetRoomState(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.RoomState, len(rows))
	for i, r := range rows {
		out[i] = storage.RoomState{
			RoomID:    r.RoomID,
			EventType: r.EventType,
			StateKey:  r.StateKey,
			Content:   r.Content,
			Sender:    r.Sender,
			Timestamp: r.Timestamp,
		}
	}
	return out, nil
}

func (s *Store) SetRoomState(ctx context.Context, p storage.SetRoomStateParams) error {
	return s.q.SetRoomState(ctx, db.SetRoomStateParams{
		RoomID:    p.RoomID,
		EventType: p.EventType,
		StateKey:  p.StateKey,
		Content:   p.Content,
		Sender:    p.Sender,
		Timestamp: p.Timestamp,
	})
}

// --- Messages ---

func (s *Store) InsertMessage(ctx context.Context, p storage.InsertMessageParams) error {
	return s.q.InsertMessage(ctx, db.InsertMessageParams{
		ID:          p.ID,
		RoomID:      p.RoomID,
		Sender:      p.Sender,
		EventType:   p.EventType,
		Content:     p.Content,
		OriginTs:    p.OriginTS,
		ServerTs:    p.ServerTS,
		VectorClock: p.VectorClock,
	})
}

func (s *Store) GetMessages(ctx context.Context, roomID string, before int64, limit int) ([]storage.Message, error) {
	rows, err := s.q.GetMessages(ctx, db.GetMessagesParams{
		RoomID:   roomID,
		ServerTs: before,
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return convMessages(rows), nil
}

func (s *Store) GetMessagesSince(ctx context.Context, roomID string, since int64, limit int) ([]storage.Message, error) {
	rows, err := s.q.GetMessagesSince(ctx, db.GetMessagesSinceParams{
		RoomID:   roomID,
		ServerTs: since,
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return convMessages(rows), nil
}

func (s *Store) GetMessageByID(ctx context.Context, id string) (storage.Message, error) {
	m, err := s.q.GetMessageByID(ctx, id)
	if err != nil {
		return storage.Message{}, err
	}
	return convMessage(m), nil
}

func (s *Store) DeleteMessage(ctx context.Context, id string) error {
	return s.q.DeleteMessage(ctx, id)
}

// --- Crypto Keys ---

func (s *Store) StorePreKey(ctx context.Context, p storage.StorePreKeyParams) error {
	isSigned := int64(0)
	if p.IsSigned {
		isSigned = 1
	}
	return s.q.StorePreKey(ctx, db.StorePreKeyParams{
		UserID:    p.UserID,
		KeyID:     p.KeyID,
		PublicKey: p.PublicKey,
		IsSigned:  isSigned,
		Signature: p.Signature,
		CreatedAt: p.CreatedAt,
	})
}

func (s *Store) ClaimPreKey(ctx context.Context, userID string) (storage.PreKey, error) {
	pk, err := s.q.ClaimPreKey(ctx, userID)
	if err != nil {
		return storage.PreKey{}, err
	}
	return convPreKey(pk), nil
}

func (s *Store) GetSignedPreKey(ctx context.Context, userID string) (storage.PreKey, error) {
	pk, err := s.q.GetSignedPreKey(ctx, userID)
	if err != nil {
		return storage.PreKey{}, err
	}
	return convPreKey(pk), nil
}

func (s *Store) CountUnclaimedPreKeys(ctx context.Context, userID string) (int64, error) {
	return s.q.CountUnclaimedPreKeys(ctx, userID)
}

// --- Sessions ---

func (s *Store) StoreSession(ctx context.Context, p storage.StoreSessionParams) error {
	return s.q.StoreSession(ctx, db.StoreSessionParams{
		ID:           p.ID,
		LocalUserID:  p.LocalUserID,
		RemoteUserID: p.RemoteUserID,
		SessionData:  p.SessionData,
		UpdatedAt:    p.UpdatedAt,
	})
}

func (s *Store) GetSession(ctx context.Context, localUserID, remoteUserID string) (storage.Session, error) {
	sess, err := s.q.GetSession(ctx, db.GetSessionParams{
		LocalUserID:  localUserID,
		RemoteUserID: remoteUserID,
	})
	if err != nil {
		return storage.Session{}, err
	}
	return storage.Session{
		ID:           sess.ID,
		LocalUserID:  sess.LocalUserID,
		RemoteUserID: sess.RemoteUserID,
		SessionData:  sess.SessionData,
		UpdatedAt:    sess.UpdatedAt,
	}, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.q.DeleteSession(ctx, id)
}

// --- Permissions ---

func (s *Store) GetInstancePerms(ctx context.Context, userID string) (uint64, error) {
	p, err := s.q.GetInstancePerms(ctx, userID)
	if err != nil {
		return 0, err
	}
	return uint64(p), nil
}

func (s *Store) GetPowerLevel(ctx context.Context, roomID, userID string) (int, error) {
	p, err := s.q.GetPowerLevel(ctx, db.GetPowerLevelParams{RoomID: roomID, UserID: userID})
	if err != nil {
		return 0, err
	}
	return int(p), nil
}

// --- To-Device Messages ---

func (s *Store) StoreToDeviceMessage(ctx context.Context, p storage.ToDeviceParams) error {
	return s.q.StoreToDeviceMessage(ctx, db.StoreToDeviceMessageParams{
		Sender:    p.Sender,
		Recipient: p.Recipient,
		EventType: p.EventType,
		Content:   p.Content,
		CreatedAt: p.CreatedAt,
	})
}

func (s *Store) GetToDeviceMessages(ctx context.Context, recipient string, limit int) ([]storage.ToDeviceMessage, error) {
	rows, err := s.q.GetToDeviceMessages(ctx, db.GetToDeviceMessagesParams{
		Recipient: recipient,
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]storage.ToDeviceMessage, len(rows))
	for i, r := range rows {
		out[i] = storage.ToDeviceMessage{
			ID:        r.ID,
			Sender:    r.Sender,
			Recipient: r.Recipient,
			EventType: r.EventType,
			Content:   r.Content,
			CreatedAt: r.CreatedAt,
		}
	}
	return out, nil
}

func (s *Store) DeleteToDeviceMessages(ctx context.Context, upToID int64, recipient string) error {
	return s.q.DeleteToDeviceMessages(ctx, db.DeleteToDeviceMessagesParams{
		ID:        upToID,
		Recipient: recipient,
	})
}

// --- Federation ---

func (s *Store) EnqueueFederationEvent(ctx context.Context, p storage.EnqueueFedEventParams) error {
	return s.q.EnqueueFederationEvent(ctx, db.EnqueueFederationEventParams{
		ID:           p.ID,
		TargetServer: p.TargetServer,
		EventData:    p.EventData,
		CreatedAt:    p.CreatedAt,
		NextRetry:    p.NextRetry,
	})
}

func (s *Store) GetPendingFederationEvents(ctx context.Context, server string, before int64, limit int) ([]storage.FederationQueueItem, error) {
	rows, err := s.q.GetPendingFederationEvents(ctx, db.GetPendingFederationEventsParams{
		TargetServer: server,
		NextRetry:    before,
		Limit:        int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]storage.FederationQueueItem, len(rows))
	for i, r := range rows {
		item := storage.FederationQueueItem{
			ID:           r.ID,
			TargetServer: r.TargetServer,
			EventData:    r.EventData,
			CreatedAt:    r.CreatedAt,
			Attempts:     int(r.Attempts),
			NextRetry:    r.NextRetry,
		}
		if r.SentAt.Valid {
			v := r.SentAt.Int64
			item.SentAt = &v
		}
		out[i] = item
	}
	return out, nil
}

func (s *Store) MarkFederationEventSent(ctx context.Context, id string, sentAt int64) error {
	return s.q.MarkFederationEventSent(ctx, db.MarkFederationEventSentParams{
		SentAt: sql.NullInt64{Int64: sentAt, Valid: true},
		ID:     id,
	})
}

func (s *Store) IncrementFederationAttempt(ctx context.Context, id string, nextRetry int64) error {
	return s.q.IncrementFederationAttempt(ctx, db.IncrementFederationAttemptParams{
		NextRetry: nextRetry,
		ID:        id,
	})
}

func (s *Store) StoreFederationServer(ctx context.Context, p storage.FederationServerParams) error {
	return s.q.StoreFederationServer(ctx, db.StoreFederationServerParams{
		Hostname:   p.Hostname,
		PublicKey:  p.PublicKey,
		VerifiedAt: p.VerifiedAt,
		LastSeen:   p.LastSeen,
	})
}

func (s *Store) GetFederationServer(ctx context.Context, hostname string) (storage.FederationServer, error) {
	fs, err := s.q.GetFederationServer(ctx, hostname)
	if err != nil {
		return storage.FederationServer{}, err
	}
	return storage.FederationServer{
		Hostname:   fs.Hostname,
		PublicKey:  fs.PublicKey,
		VerifiedAt: fs.VerifiedAt,
		LastSeen:   fs.LastSeen,
	}, nil
}

// --- Media ---

func (s *Store) StoreMedia(ctx context.Context, p storage.StoreMediaParams) error {
	return s.q.StoreMedia(ctx, db.StoreMediaParams{
		ID:          p.ID,
		UploaderID:  p.UploaderID,
		Filename:    p.Filename,
		ContentType: p.ContentType,
		SizeBytes:   p.SizeBytes,
		StoragePath: p.StoragePath,
		CreatedAt:   p.CreatedAt,
	})
}

func (s *Store) GetMedia(ctx context.Context, id string) (storage.MediaMeta, error) {
	m, err := s.q.GetMedia(ctx, id)
	if err != nil {
		return storage.MediaMeta{}, err
	}
	return storage.MediaMeta{
		ID:          m.ID,
		UploaderID:  m.UploaderID,
		Filename:    m.Filename,
		ContentType: m.ContentType,
		SizeBytes:   m.SizeBytes,
		StoragePath: m.StoragePath,
		CreatedAt:   m.CreatedAt,
	}, nil
}

// --- Conversion helpers ---

func convUser(u db.User) storage.User {
	return storage.User{
		ID:            u.ID,
		Handle:        u.Handle,
		DisplayName:   u.DisplayName,
		AvatarURL:     u.AvatarUrl,
		IdentityKey:   u.IdentityKey,
		PasswordHash:  u.PasswordHash,
		InstancePerms: uint64(u.InstancePerms),
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

func convRoom(r db.Room) storage.Room {
	return storage.Room{
		ID:        r.ID,
		Name:      r.Name,
		Topic:     r.Topic,
		AvatarURL: r.AvatarUrl,
		IsDirect:  r.IsDirect != 0,
		RoomType:  r.RoomType,
		CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt,
	}
}

func convMessage(m db.Message) storage.Message {
	return storage.Message{
		ID:          m.ID,
		RoomID:      m.RoomID,
		Sender:      m.Sender,
		EventType:   m.EventType,
		Content:     m.Content,
		OriginTS:    m.OriginTs,
		ServerTS:    m.ServerTs,
		VectorClock: m.VectorClock,
	}
}

func convMessages(rows []db.Message) []storage.Message {
	out := make([]storage.Message, len(rows))
	for i, m := range rows {
		out[i] = convMessage(m)
	}
	return out
}

func convPreKey(pk db.Prekey) storage.PreKey {
	return storage.PreKey{
		ID:        pk.ID,
		UserID:    pk.UserID,
		KeyID:     pk.KeyID,
		PublicKey: pk.PublicKey,
		IsSigned:  pk.IsSigned != 0,
		Signature: pk.Signature,
		Claimed:   pk.Claimed != 0,
		CreatedAt: pk.CreatedAt,
	}
}
