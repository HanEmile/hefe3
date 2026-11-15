-- +goose Up

CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    handle          TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    identity_key    BLOB NOT NULL,
    password_hash   TEXT NOT NULL,
    instance_perms  INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE TABLE user_tags (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (user_id, tag)
);

CREATE TABLE rooms (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    topic       TEXT NOT NULL DEFAULT '',
    avatar_url  TEXT NOT NULL DEFAULT '',
    is_direct   INTEGER NOT NULL DEFAULT 0,
    room_type   TEXT NOT NULL DEFAULT 'chat',
    created_by  TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE room_members (
    room_id     TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    power_level INTEGER NOT NULL DEFAULT 0,
    joined_at   INTEGER NOT NULL,
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE room_state (
    room_id     TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    state_key   TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    sender      TEXT NOT NULL,
    timestamp   INTEGER NOT NULL,
    PRIMARY KEY (room_id, event_type, state_key)
);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    room_id         TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    content         TEXT NOT NULL,
    origin_ts       INTEGER NOT NULL,
    server_ts       INTEGER NOT NULL,
    vector_clock    TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_messages_room_ts ON messages(room_id, server_ts);
CREATE INDEX idx_messages_room_id ON messages(room_id, id);

CREATE TABLE prekeys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id      INTEGER NOT NULL,
    public_key  BLOB NOT NULL,
    is_signed   INTEGER NOT NULL DEFAULT 0,
    signature   BLOB,
    claimed     INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
CREATE INDEX idx_prekeys_user_unclaimed ON prekeys(user_id, claimed) WHERE claimed = 0;

CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    local_user_id   TEXT NOT NULL,
    remote_user_id  TEXT NOT NULL,
    session_data    BLOB NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_sessions_pair ON sessions(local_user_id, remote_user_id);

CREATE TABLE federation_queue (
    id              TEXT PRIMARY KEY,
    target_server   TEXT NOT NULL,
    event_data      TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_retry      INTEGER NOT NULL,
    sent_at         INTEGER
);
CREATE INDEX idx_fed_queue_pending ON federation_queue(target_server, next_retry) WHERE sent_at IS NULL;

CREATE TABLE federation_servers (
    hostname    TEXT PRIMARY KEY,
    public_key  BLOB NOT NULL,
    verified_at INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL
);

CREATE TABLE media (
    id              TEXT PRIMARY KEY,
    uploader_id     TEXT NOT NULL,
    filename        TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      INTEGER NOT NULL,
    storage_path    TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);

CREATE TABLE to_device_messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sender      TEXT NOT NULL,
    recipient   TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX idx_to_device_recipient ON to_device_messages(recipient);

-- +goose Down

DROP TABLE IF EXISTS to_device_messages;
DROP TABLE IF EXISTS media;
DROP TABLE IF EXISTS federation_servers;
DROP TABLE IF EXISTS federation_queue;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS prekeys;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS room_state;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS user_tags;
DROP TABLE IF EXISTS users;
