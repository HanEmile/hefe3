-- name: StoreMedia :exec
INSERT INTO media (id, uploader_id, filename, content_type, size_bytes, storage_path, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetMedia :one
SELECT * FROM media WHERE id = ?;

-- name: DeleteMedia :exec
DELETE FROM media WHERE id = ?;
