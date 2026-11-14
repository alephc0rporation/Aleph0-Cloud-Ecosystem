-- name: LockUser :exec
-- Serializes one user's tenant creations until the transaction ends.
SELECT pg_advisory_xact_lock(hashtext(sqlc.arg(sub)::text));

-- name: LockTenant :one
-- Serializes project creation and tenant deletion in one tenant until the transaction ends.
SELECT id FROM tenants WHERE id = $1 FOR UPDATE;

-- name: InsertTenant :one
INSERT INTO tenants (name, created_by) VALUES ($1, $2) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = $1;

-- name: ListTenantsForUser :many
SELECT t.id, t.name, t.created_at, m.role
FROM tenants t JOIN memberships m ON m.tenant_id = t.id
WHERE m.user_sub = $1
ORDER BY t.created_at;

-- name: CountTenantsCreatedBy :one
SELECT count(*) FROM tenants WHERE created_by = $1;

-- name: DeleteTenant :exec
DELETE FROM tenants WHERE id = $1;
