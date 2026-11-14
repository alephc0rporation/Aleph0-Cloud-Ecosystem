-- +goose Up
CREATE TYPE project_status AS ENUM ('provisioning', 'ready', 'failed', 'deleting');

CREATE TABLE tenants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tenants_created_by ON tenants (created_by);

CREATE TABLE memberships (
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    user_sub   text NOT NULL,
    role       text NOT NULL CHECK (role IN ('owner', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_sub)
);
CREATE INDEX memberships_user_sub ON memberships (user_sub);

CREATE TABLE projects (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants (id),
    name                 text NOT NULL,
    openstack_project_id text,
    status               project_status NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

-- +goose Down
DROP TABLE projects;
DROP TABLE memberships;
DROP TABLE tenants;
DROP TYPE project_status;
