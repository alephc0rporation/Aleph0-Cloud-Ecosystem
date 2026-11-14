# Aleph0 Cardinal

Our own cloud platform on OpenStack: a web portal and a public API in front of VMs, storage,
networks and load balancers. See [docs/architecture.md](docs/architecture.md).

## Prerequisites

Development happens in **WSL2 (Ubuntu)** on Windows, with the repo in the WSL filesystem
(`~/projects/`), not under `/mnt/c`.

- `C:\Users\<you>\.wslconfig`:

  ```ini
  [wsl2]
  networkingMode=mirrored
  memory=24GB
  ```

  then `wsl --shutdown` from PowerShell.
- Docker Desktop with WSL integration enabled for your Ubuntu distribution
  (Settings → Resources → WSL integration).
- In WSL: `make`, Go 1.27 (`linux/amd64`), Node 24 LTS.

## First run

```bash
cp .env.example .env
make up      # PostgreSQL + Keycloak, waits until both are healthy
make dev     # applies migrations, then the API on :8080 and web on :5173; Ctrl+C stops both
```

The API needs the OpenStack settings in `.env`. With the dev cluster deployed (`deploy/README.md`),
fill the two service users' passwords:

```bash
sed -i "s|^OS_API_PASSWORD=.*|OS_API_PASSWORD=$(cat deploy/kolla/dev/credentials/aleph0-api)|; s|^OS_PROVISIONER_PASSWORD=.*|OS_PROVISIONER_PASSWORD=$(cat deploy/kolla/dev/credentials/aleph0-provisioner)|" .env
```

Tenants work without the cluster; creating and deleting projects needs it running
(`make infra-start`), and answers 503 while it is stopped.

Open http://localhost:5173 (exactly `localhost`: Keycloak only accepts that redirect) and log in
as `dev@aleph0.local` / `dev`. A second user, `dev2@aleph0.local` / `dev`, shows that tenants
are private. The dev-only Keycloak client `cli` allows a password login for scripted checks:

```bash
curl -s -d grant_type=password -d client_id=cli -d username=dev@aleph0.local -d password=dev \
  http://localhost:8081/realms/aleph0/protocol/openid-connect/token
```

| Service | URL |
|---|---|
| Web | http://localhost:5173 |
| API | http://localhost:8080 (`/healthz`, `/v1/...`) |
| Keycloak admin | http://localhost:8081 (`admin` / `admin`) |
| PostgreSQL | `localhost:5432`, database `api`, user `api` / `api` |

## Commands

| Command | Does |
|---|---|
| `make up` | Start PostgreSQL and Keycloak |
| `make dev` | Run the API and the web dev server |
| `make migrate` | Apply database migrations (`make dev` runs it first) |
| `make generate` | Bundle `api/openapi/` into `api/openapi.yaml`, then regenerate the Go and TypeScript code and the sqlc queries |
| `make test` | Lint the API contract, Go vet + tests (with the race detector), ESLint, TypeScript check, Vitest |
| `make test-integration` | Also the store tests against the compose PostgreSQL and the OpenStack adapter against the dev cluster; each skips when its settings are missing. `TEST_FLAGS="-run Name"` runs one test |
| `make down` | Stop the containers |
| `make reset` | Delete all local data and start again (re-imports the Keycloak realm) |

## Changing the API

The contract lives in `api/openapi/`, split by area (`paths/`, `components/`). Edit those files,
run `make generate` (it bundles them into `api/openapi.yaml`, which is generated too), then
implement the new method of `oapi.StrictServerInterface` in `api/internal/handlers`. The frontend gets a matching
TanStack Query hook in `web/src/api/gen/`. Never edit generated files by hand.

## Troubleshooting

- **Realm changes are ignored.** Keycloak imports `deploy/keycloak/realm-dev.json` only when the
  realm does not exist yet. Run `make reset`.
- **Port already in use.** Something on Windows or WSL holds 5173, 8080, 8081 or 5432
  (with mirrored networking they share one port space). Find it with `ss -ltnp | grep <port>`
  in WSL or `Get-NetTCPConnection -LocalPort <port>` in PowerShell.
- **401 on every API call after login.** Check that the token's `iss` is
  `http://localhost:8081/realms/aleph0` and its `aud` contains `api`; the API logs the reason
  as `access token rejected`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Licensed under the [Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
