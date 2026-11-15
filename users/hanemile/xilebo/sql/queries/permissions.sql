-- name: GetInstancePerms :one
SELECT instance_perms FROM users WHERE id = ?;

-- name: GetPowerLevel :one
SELECT power_level FROM room_members WHERE room_id = ? AND user_id = ?;

-- name: StoreToDeviceMessage :exec
INSERT INTO to_device_messages (sender, recipient, event_type, content, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetToDeviceMessages :many
SELECT * FROM to_device_messages WHERE recipient = ? ORDER BY id ASC LIMIT ?;

-- name: DeleteToDeviceMessages :exec
DELETE FROM to_device_messages WHERE id <= ? AND recipient = ?;
