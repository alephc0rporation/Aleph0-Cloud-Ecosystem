# Tenants, projects and regions — design

Issue: #20 (part of #1, blocks #4; needs #19 to run both stacks at once)
Date: 2026-11-09

## Goal

Customers organize their resources like in a public cloud. A **tenant** is a customer account with members. It holds **projects**, and every resource lives in a project and a **region**. This step builds the model, the API and the portal screens for tenants and projects. VMs come in #4, on top of it.

## Decisions

| Topic | Decision | Why |
|---|---|---|
| Hierarchy | Tenant → project → resource | One grouping level is enough to start; no subscriptions or resource groups |
| Tenant creation | Self-service: any signed-in user creates one and becomes its owner | Like signing up to a public cloud; a user can belong to several tenants |
| Roles | `owner` and `member`, at tenant level only | Per-project roles can be added later without changing these |
| Tenant in OpenStack | None: a tenant exists only in our DB | OpenStack never needs it; creating a tenant stays a fast DB write |
| Project in OpenStack | One OpenStack project per project, in the Keystone domain `customers`, tagged `tenant=<tenant id>` | Separate quotas, networks and access per project; the tag lets admins list one tenant's projects (`openstack project list --tags tenant=<id>`). A domain per tenant was rejected: creating domains needs cloud admin, it adds no isolation between customers, and Keystone cannot move a project between domains later |
| Creating OpenStack projects | User `aleph0-provisioner`, with the `manager` role on domain `customers` only | Keystone's domain manager can create projects in its domain and nothing else in the cloud; answers the "creating projects" open question in `architecture.md` |
| Other OpenStack calls | User `aleph0-api` (role `member`), with a token scoped to one project | As `architecture.md` already says |
| Identity | No Keycloak–Keystone federation | Customers never reach OpenStack; access rules live in one place, our DB |
| Network | Every project gets a network, subnet and router named `default` in every region, created with the project | A VM can be created right away; custom networks come later |
| Regions | From API config; one today (`ro-1`) | A region is a cluster; one Keystone can serve several regions later, so a project can span them |
| Provisioning | Synchronous, inside the request; every step idempotent | A few seconds per project; no job worker yet |

`architecture.md` changes with this step: "organization" becomes **tenant**, the link from an organization to a Keycloak group is dropped (memberships in our DB are the only source for access checks), and the "creating projects" open question gets the answer above.

## Concepts

- **Tenant**: has a name, members (owner or member) and projects.
- **Project**: belongs to one tenant; name unique within it. Has a status: `provisioning`, `ready`, `failed` or `deleting`.
- **Region**: an OpenStack region of one cluster. Its public ID (`ro-1`) and display name ("Romania 1") come from config, with the OpenStack region name (`RegionOne`) and the external network (`public`) behind them.
- **Resources** (VMs, from #4): not stored by us; OpenStack is the source of truth. A resource belongs to a project because it lives in that project's OpenStack project.

## Infra changes (`deploy/playbooks/openstack-init.yml`)

- Domain `customers`.
- User `aleph0-provisioner` in domain `Default`, with a generated password saved to `deploy/kolla/dev/credentials/aleph0-provisioner` (the same pattern as `aleph0-api`), and the `manager` role on domain `customers`.
- `aleph0-api` gets nothing new here; it gets the `member` role on each project as the project is created.
- The `dev` project and its network stay, for manual OpenStack tests.

**To verify first:** that Keystone 2026.2 lets a domain `manager` give a user from domain `Default` the `member` role on a project in `customers`. If it does not, both service users move into `customers`, or a small policy override allows that one grant.

## Data

PostgreSQL. This step brings goose (migrations embedded in the binary, run by `api migrate`) and sqlc, as the app skeleton planned.

| Table | Columns | Constraints |
|---|---|---|
| `tenants` | `id` uuid (`uuidv7()`), `name` text, `created_by` text (Keycloak `sub`), `created_at` | |
| `memberships` | `tenant_id`, `user_sub` text, `role` text (`owner` / `member`), `created_at` | PK (`tenant_id`, `user_sub`); FK to `tenants` |
| `projects` | `id` uuid (`uuidv7()`), `tenant_id`, `name`, `openstack_project_id` text (null until created), `status` text, `created_at` | Unique (`tenant_id`, `name`); FK to `tenants` |

Names are 1–64 characters after trimming; the OpenAPI spec enforces it.

In OpenStack, a project's Keystone project is named after our project ID, tagged `tenant=<tenant id>`, with `<project name>` as its description, for admins.

## Backend

### Contract (`api/openapi.yaml`)

| Endpoint | Who | Responses |
|---|---|---|
| `GET /v1/regions` | any signed-in user | `200 [{id, name}]` |
| `GET /v1/tenants` | any signed-in user | `200 [{id, name, role, createdAt}]`, the caller's tenants |
| `POST /v1/tenants` `{name}` | any signed-in user | `201` tenant, caller is `owner`; `403` limit reached |
| `GET /v1/tenants/{tenantId}` | member | `200` tenant; `404` |
| `DELETE /v1/tenants/{tenantId}` | owner | `204`; `409` while it has projects; `403` for a member who is not an owner; `404` |
| `GET /v1/tenants/{tenantId}/projects` | member | `200 [{id, tenantId, name, status, createdAt}]` |
| `POST /v1/tenants/{tenantId}/projects` `{name}` | member | `201` project (`ready`); `403` limit; `409` duplicate name; `502` OpenStack error (project left `failed`); `503` region unavailable |
| `GET /v1/projects/{projectId}` | member of its tenant | `200` project; `404` |
| `DELETE /v1/projects/{projectId}` | member of its tenant | `204`; `409` while it has VMs; `502`; `503`; `404` |

- **Not a member → `404`**, never `403`, so the API does not reveal that a tenant or project exists. `403` is only for a member who lacks the role (deleting a tenant as `member`) or for a limit.
- Project IDs are unique across tenants, so routes below a project do not repeat the tenant; the handler loads the project, then checks membership in its tenant.
- #4 adds `/v1/projects/{projectId}/regions/{region}/servers` and `/v1/regions/{region}/flavors|images`.

### Authorization

One function per check, used by every handler: `memberOf(tenant, sub)` returns the role or "not a member". No middleware magic: each handler states which role it needs.

Two layers keep customers apart:

1. **Our API**: the membership check above, on every tenant and project route. Customers have no OpenStack identity, so this is the only way in.
2. **OpenStack scoping**: every call uses a token scoped to one project, so a mistake in layer 1 that passes another project's VM ID still gets "not found" from Nova. Networks are separate per project.

Rules that keep both layers working:

- The OpenStack project (and so the scoped token) always comes from our DB row of the project the caller is a member of, never from request data.
- The `tenant=` tag is a label for admins. Access decisions never read it.
- A test calls every tenant and project route as a non-member and expects `404`, so a route added without the check fails (see Testing).

Weak spot, accepted: `aleph0-api` is `member` on every customer project, and `aleph0-provisioner` could give itself the same, so either password opens all customer resources (not the `Default` domain, where the admin project and OpenStack's own service users live). This comes from not federating identities. Mitigations: OpenStack APIs reachable only from the backend (over the VPN in production), passwords in `.env` in dev and Secret Manager in production, and rotation by setting a new password and restarting the API.

### Flows

**Create a tenant**: one transaction takes a lock per user (`pg_advisory_xact_lock(hashtext(sub))`, released at commit), checks the caller has created fewer than `LIMIT_TENANTS_PER_USER` tenants, and inserts the tenant and the caller's `owner` membership. Without the lock, parallel requests from one user would all pass the count and bypass the limit.

**Create a project**:

1. Transaction: lock the tenant row (`SELECT … FOR UPDATE`), check the project count against `LIMIT_PROJECTS_PER_TENANT`, insert the project as `provisioning`. A duplicate name fails on the unique constraint → `409`.
2. As `aleph0-provisioner`: find or create the Keystone project in `customers`, named after the project ID and tagged `tenant=<tenant id>`; give `aleph0-api` the `member` role on it; save `openstack_project_id`.
3. For each region, as `aleph0-api` scoped to the new project: find or create network `default`, subnet `default` (`192.168.0.0/24`, like `dev-subnet`), router `default` with its gateway on the region's external network and an interface on the subnet.
4. Set `ready`; return `201`.

On an error in steps 2–3: set `failed`, log the error with the request ID, and return `502` (OpenStack answered with an error) or `503` (timeout or connection refused). The user deletes the failed project and creates it again. There is no retry endpoint.

The whole create, and the whole delete below, run with a 25 s budget, under the server's 30 s write timeout.

**Delete a project**:

1. Set `deleting`, from `ready`, `failed` or `deleting`. A project that is `provisioning` returns `409`, unless it has been `provisioning` for more than 5 minutes: then the API stopped in the middle of creating it, and it counts as `failed`. #4 refuses new VMs in a project that is not `ready`.
2. In each region, count the project's servers; if any, put the previous status back and return `409`.
3. Remove the router's interface, the router, the subnet and the network, then the Keystone project (as `aleph0-provisioner`), found by its name (our project ID). Anything already missing counts as removed. When `openstack_project_id` is empty, creation stopped before any network existed, so steps 2–3 only look for the Keystone project: a timeout can leave one created in Keystone but never saved.
4. Delete the row; return `204`.

A failure in step 3 leaves the project `deleting`, and a new `DELETE` continues where it stopped. A VM created just before `deleting` makes the network removal fail (its port is still in use): nothing is lost, and the user deletes the VM and retries.

**Delete a tenant**: owner only; one transaction deletes the memberships and the tenant if it has no projects, otherwise `409`.

### OpenStack adapter (`internal/openstack`)

Gophercloud v2, behind a small interface that the handlers use and the tests fake:

```go
type Cloud interface {
    CreateProject(ctx, projectID, description string) (openstackProjectID string, err error)
    DeleteProject(ctx, projectID string) error // finds the Keystone project by name
    EnsureDefaultNetwork(ctx, region, openstackProjectID string) error
    DeleteDefaultNetwork(ctx, region, openstackProjectID string) error
    CountServers(ctx, region, openstackProjectID string) (int, error)
}
```

- Every call finds by name before creating and treats "not found" as done when deleting, so each one is safe to repeat.
- Clients: one for `aleph0-provisioner` (domain-scoped) and one per OpenStack project for `aleph0-api` (project-scoped), cached in memory, with Gophercloud's re-authentication on, so a token expiring after an hour does not break calls. *shortcut: the per-project client cache only grows; fine for hundreds of projects, revisit with many more.*
- Timeouts: 10 s per OpenStack call. Errors are classified as `unavailable` (timeout, connection refused → `503`) or `failed` (any other → `502`).
- The external network ID per region is looked up by name once and cached.

### Config (`internal/config`)

| Variable | Required | Example |
|---|---|---|
| `OS_AUTH_URL` | yes | `http://10.10.0.10:5000/v3` |
| `OS_API_USERNAME`, `OS_API_PASSWORD` | yes | `aleph0-api`, from `deploy/kolla/dev/credentials/aleph0-api` |
| `OS_PROVISIONER_USERNAME`, `OS_PROVISIONER_PASSWORD` | yes | `aleph0-provisioner`, from its credentials file |
| `REGIONS` | yes | `[{"id":"ro-1","name":"Romania 1","openstack_region":"RegionOne","external_network":"public"}]` |
| `LIMIT_TENANTS_PER_USER` | no | `3` |
| `LIMIT_PROJECTS_PER_TENANT` | no | `10` |

Missing or malformed values stop the process at startup with one message, as today. `.env.example` lists them and says where the passwords come from.

### Commands

`api migrate` applies pending migrations and exits. `make migrate` runs it, and `make dev` runs it before starting the API.

## Frontend

- **Routes** (TanStack Router, file-based):
  - `/` sends the user to their first tenant, or to `/tenants/new` when they have none
  - `/tenants/new`: name form; creates the tenant and opens it
  - `/t/$tenantId/projects`: project list (name, status, created), "New project" form
  - `/t/$tenantId/projects/$projectId`: name, status, regions; #4 adds the VM list here
  - `/t/$tenantId/settings`: delete the tenant (owner only)
- **Tenant switcher** in the header: the caller's tenants with their role, and "Create tenant". The current tenant is in the URL, so links and reloads keep it.
- Creating a project shows a pending state on the button until the request returns (a few seconds).
- A `failed` project shows its status and a delete button. A `409` on delete shows the API's message.
- Forms use react-hook-form with the Zod schemas generated by orval.

## Errors

RFC 9457 problem details, as today.

| Status | When |
|---|---|
| 400 | The request does not match the OpenAPI spec |
| 401 | Missing or invalid token |
| 403 | A limit is reached, or the caller's role is not enough |
| 404 | Not found, or the caller is not a member |
| 409 | Duplicate project name; deleting a tenant with projects or a project with VMs; acting on a project that is `deleting` |
| 502 | OpenStack returned an error |
| 503 | The region did not answer in time |

OpenStack's error details go to the API log with the request ID. The response carries a short message and the request ID, never OpenStack internals or credentials.

## Testing

**Go, in `make test`** (no external services):

- Every tenant and project route, listed in one table, called as a non-member → `404`; a new route must be added to the table (the test fails when the spec has a `/v1/tenants/…` or `/v1/projects/…` route the table lacks)
- Handlers against a fake `Cloud` and a fake store:
  - membership: member, owner, non-member (`404`)
  - limits (`403`), duplicate name (`409`)
  - project creation failing at each step → `failed`, `502` or `503`
  - project deletion: VMs present (`409`, back to `ready`), failure halfway (stays `deleting`), second `DELETE` finishes
- Config: `REGIONS` parsing and the new required variables

**Go, opt-in** (`make test-integration`, build tag `integration`; skipped when its variables are missing):

- Store queries against the compose Postgres, in a database created and dropped by the test; 10 parallel tenant creations by one user stop at the limit
- The OpenStack adapter against the dev cluster: create a project and its default network twice (the second run creates nothing), delete twice; `DeleteProject` removes a Keystone project whose ID was never saved; with the cluster stopped, calls fail as `unavailable` within the timeout
- Re-authentication: a local fake Keystone rejects the first token; the client logs in again

**Web** (Vitest): the new-tenant screen, the project list states (`provisioning`, `failed`, a `409` message), the tenant switcher.

The dev Keycloak realm gets a second user, `dev2@aleph0.local`, for the isolation check.

## Documentation

- `docs/architecture.md`: tenant naming, the model above, the answered open question, the `memberships`/`projects` tables in section 7
- `README.md`: the new variables and how to fill them, `make migrate`, `make test-integration`

## Acceptance criteria

On the dev cluster with the app stack running:

1. A new user creates a tenant, then a project. Within 10 s it is `ready`, and OpenStack shows its project in `customers` with network, subnet and router `default`.
2. `dev2@aleph0.local` sees none of it; opening the first user's tenant and project URLs gives `404`.
3. Deleting the project removes its router, network and OpenStack project; deleting the tenant then works.
4. The tenant and project limits return `403`.
5. With the cluster stopped, creating a project returns `503` within 15 s and leaves it `failed`, and creating a tenant still works.
6. No OpenStack credentials or tokens reach the browser.
7. `make test` passes; `make test-integration` passes with the cluster and the app stack up.

## Out of scope

- Inviting and removing members, renaming, per-project roles
- VMs (#4), quotas beyond the two limits, billing, audit log (`resource_events`)
- More than one region
- A job worker (provisioning stays synchronous)
