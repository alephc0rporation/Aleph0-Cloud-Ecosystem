-- name: InsertProject :one
INSERT INTO projects (tenant_id, name, status) VALUES ($1, $2, 'provisioning') RETURNING *;

-- name: ListProjects :many
SELECT * FROM projects WHERE tenant_id = $1 ORDER BY created_at;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectForUpdate :one
SELECT * FROM projects WHERE id = $1 FOR UPDATE;

-- name: CountProjects :one
SELECT count(*) FROM projects WHERE tenant_id = $1;

-- name: SetOpenStackProjectID :exec
UPDATE projects SET openstack_project_id = $2, updated_at = now() WHERE id = $1;

-- name: SetProjectStatus :exec
UPDATE projects SET status = $2, updated_at = now() WHERE id = $1;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;
