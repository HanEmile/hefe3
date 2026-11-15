-- name: EnqueueFederationEvent :exec
INSERT INTO federation_queue (id, target_server, event_data, created_at, attempts, next_retry)
VALUES (?, ?, ?, ?, 0, ?);

-- name: GetPendingFederationEvents :many
SELECT * FROM federation_queue
WHERE target_server = ? AND sent_at IS NULL AND next_retry <= ?
ORDER BY created_at ASC
LIMIT ?;

-- name: MarkFederationEventSent :exec
UPDATE federation_queue SET sent_at = ? WHERE id = ?;

-- name: IncrementFederationAttempt :exec
UPDATE federation_queue SET attempts = attempts + 1, next_retry = ? WHERE id = ?;

-- name: StoreFederationServer :exec
INSERT INTO federation_servers (hostname, public_key, verified_at, last_seen)
VALUES (?, ?, ?, ?)
ON CONFLICT (hostname) DO UPDATE
SET public_key = excluded.public_key, verified_at = excluded.verified_at, last_seen = excluded.last_seen;

-- name: GetFederationServer :one
SELECT * FROM federation_servers WHERE hostname = ?;

-- name: CleanOldFederationEvents :exec
DELETE FROM federation_queue WHERE sent_at IS NOT NULL AND sent_at < ?;
