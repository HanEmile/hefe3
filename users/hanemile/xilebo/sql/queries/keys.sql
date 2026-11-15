-- name: StorePreKey :exec
INSERT INTO prekeys (user_id, key_id, public_key, is_signed, signature, claimed, created_at)
VALUES (?, ?, ?, ?, ?, 0, ?);

-- name: ClaimPreKey :one
UPDATE prekeys SET claimed = 1
WHERE id = (
    SELECT pk.id FROM prekeys pk
    WHERE pk.user_id = ? AND pk.claimed = 0 AND pk.is_signed = 0
    LIMIT 1
)
RETURNING *;

-- name: GetSignedPreKey :one
SELECT * FROM prekeys
WHERE user_id = ? AND is_signed = 1
ORDER BY created_at DESC
LIMIT 1;

-- name: CountUnclaimedPreKeys :one
SELECT COUNT(*) FROM prekeys WHERE user_id = ? AND claimed = 0 AND is_signed = 0;

-- name: StoreSession :exec
INSERT INTO sessions (id, local_user_id, remote_user_id, session_data, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET session_data = excluded.session_data, updated_at = excluded.updated_at;

-- name: GetSession :one
SELECT * FROM sessions WHERE local_user_id = ? AND remote_user_id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
