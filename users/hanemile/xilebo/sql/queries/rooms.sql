-- name: CreateRoom :one
INSERT INTO rooms (id, name, topic, avatar_url, is_direct, room_type, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRoom :one
SELECT * FROM rooms WHERE id = ?;

-- name: ListUserRooms :many
SELECT r.* FROM rooms r
JOIN room_members rm ON rm.room_id = r.id
WHERE rm.user_id = ?
ORDER BY r.name;

-- name: AddMember :exec
INSERT INTO room_members (room_id, user_id, power_level, joined_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (room_id, user_id) DO UPDATE SET power_level = excluded.power_level;

-- name: RemoveMember :exec
DELETE FROM room_members WHERE room_id = ? AND user_id = ?;

-- name: GetMembers :many
SELECT * FROM room_members WHERE room_id = ? ORDER BY user_id;

-- name: GetMember :one
SELECT * FROM room_members WHERE room_id = ? AND user_id = ?;

-- name: SetPowerLevel :exec
UPDATE room_members SET power_level = ? WHERE room_id = ? AND user_id = ?;

-- name: GetRoomState :many
SELECT * FROM room_state WHERE room_id = ?;

-- name: SetRoomState :exec
INSERT INTO room_state (room_id, event_type, state_key, content, sender, timestamp)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (room_id, event_type, state_key) DO UPDATE
SET content = excluded.content, sender = excluded.sender, timestamp = excluded.timestamp;
