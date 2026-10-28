# Aleph0 Cloud

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
make dev     # API on :8080 and web on :5173; Ctrl+C stops both
```

Open http://localhost:5173 and log in as `dev@aleph0.local` / `dev`.

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
| `make generate` | Regenerate Go and TypeScript code after editing `api/openapi.yaml` |
| `make test` | Go vet + tests, ESLint, TypeScript check, Vitest |
| `make down` | Stop the containers |
| `make reset` | Delete all local data and start again (re-imports the Keycloak realm) |

## Changing the API

`api/openapi.yaml` is the contract. Edit it, run `make generate`, then implement the new method
of `oapi.StrictServerInterface` in `api/internal/handlers`. The frontend gets a matching
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
