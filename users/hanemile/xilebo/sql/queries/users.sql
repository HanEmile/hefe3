-- name: CreateUser :one
INSERT INTO users (id, handle, display_name, avatar_url, identity_key, password_hash, instance_perms, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByHandle :one
SELECT * FROM users WHERE handle = ?;

-- name: UpdateProfile :exec
UPDATE users SET display_name = ?, avatar_url = ?, updated_at = ? WHERE id = ?;

-- name: UpdateInstancePerms :exec
UPDATE users SET instance_perms = ?, updated_at = ? WHERE id = ?;

-- name: SetUserTags :exec
DELETE FROM user_tags WHERE user_id = ?;

-- name: InsertUserTag :exec
INSERT INTO user_tags (user_id, tag) VALUES (?, ?);

-- name: GetUserTags :many
SELECT tag FROM user_tags WHERE user_id = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY handle LIMIT ? OFFSET ?;
