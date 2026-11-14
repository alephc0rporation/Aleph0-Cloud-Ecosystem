-- name: InsertMembership :exec
INSERT INTO memberships (tenant_id, user_sub, role) VALUES ($1, $2, $3);

-- name: GetRole :one
SELECT role FROM memberships WHERE tenant_id = $1 AND user_sub = $2;

-- name: DeleteMemberships :exec
DELETE FROM memberships WHERE tenant_id = $1;
