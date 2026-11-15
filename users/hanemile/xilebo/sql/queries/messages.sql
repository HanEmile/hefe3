-- name: InsertMessage :exec
INSERT INTO messages (id, room_id, sender, event_type, content, origin_ts, server_ts, vector_clock)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMessages :many
SELECT * FROM messages
WHERE room_id = ? AND server_ts < ?
ORDER BY server_ts DESC
LIMIT ?;

-- name: GetMessagesSince :many
SELECT * FROM messages
WHERE room_id = ? AND server_ts > ?
ORDER BY server_ts ASC
LIMIT ?;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = ?;

-- name: DeleteMessage :exec
DELETE FROM messages WHERE id = ?;
