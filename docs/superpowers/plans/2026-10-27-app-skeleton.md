# App Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A local stack where a user logs in through Keycloak in the React app, the Go API validates the token on `GET /v1/me`, and `GET /healthz` reports the PostgreSQL connection (issue #3).

**Architecture:** PostgreSQL and Keycloak run in Docker Compose; the Go API and the Vite dev server run directly in WSL. `api/openapi.yaml` is the single contract: `oapi-codegen` generates the Go server interface, `orval` generates the TanStack Query hooks and Zod schemas. Everything under `/v1/` sits behind a JWT middleware that checks signature, issuer, audience and expiry.

**Tech Stack:** Go 1.27 (`net/http`, `log/slog`), oapi-codegen v2, coreos/go-oidc v3, pgx v5; React + Vite + TypeScript, TanStack Router, TanStack Query, oidc-client-ts + react-oidc-context, orval, Zod, shadcn/ui (Tailwind CSS v4), Vitest; PostgreSQL 18, Keycloak 26.

**Spec:** `docs/superpowers/specs/2026-10-27-app-skeleton-design.md`

## Global Constraints

- Everything written to the repo is in English: code, comments, docs, commit messages.
- Work happens in WSL (Ubuntu 26.04), in `~/projects/Aleph0-Cloud-Ecosystem`, on branch `feat/3-app-skeleton`. Never under `/mnt/c`.
- Go module path: `github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api`. Go 1.27, `linux/amd64`.
- Node 24 LTS, npm (no pnpm or yarn).
- Ports: web 5173, API 8080, Keycloak 8081, PostgreSQL 5432.
- Keycloak realm `aleph0`; clients `web` (public, PKCE S256) and `api` (audience only); test user `dev@aleph0.local` / `dev`.
- Issuer everywhere: `http://localhost:8081/realms/aleph0`. Audience: `api`.
- Generated code is committed and never edited by hand: `api/internal/oapi/`, `web/src/api/gen/`, `web/src/routeTree.gen.ts`.
- Errors returned by the API are RFC 9457 problem details, written only through `problem.Write`.
- Logging uses `log/slog`, always the `...Context` variants inside request handling (`slog.InfoContext(ctx, ...)`), so the request ID is attached.
- No dependencies beyond the ones named in this plan, plus whatever the Vite template and `shadcn init` / `shadcn add` install for themselves.
- Not in this plan: goose, sqlc, `api migrate`, worker, Zustand, CI, CORS, OpenStack.
- Commit messages use Conventional Commits prefixes (`feat:`, `chore:`, `docs:`, `test:`).

---

## File map

| File | Responsibility |
|---|---|
| `.gitattributes` | Force LF line endings (Makefile and shell break with CRLF) |
| `.env.example` | All dev settings; copied to `.env` |
| `Makefile` | `up`, `down`, `reset`, `dev`, `generate`, `test` |
| `docker-compose.yml` | PostgreSQL + Keycloak |
| `deploy/postgres/init.sql` | Creates `api` and `keycloak` databases and users |
| `deploy/keycloak/realm-dev.json` | Realm `aleph0`: clients, audience mapper, test user |
| `api/openapi.yaml` | API contract |
| `api/oapi-codegen.yaml` | Generator config |
| `api/internal/oapi/oapi.gen.go` | Generated server interface and models |
| `api/internal/config/config.go` | Env vars → `Config`, validation |
| `api/internal/problem/problem.go` | `Write(w, status, detail)` |
| `api/internal/auth/auth.go` | `Verifier`, `Middleware`, `Claims`, context helpers |
| `api/internal/handlers/handlers.go` | `GetHealthz`, `GetMe` |
| `api/internal/server/server.go` | Root handler (routing, auth wiring), `Run` |
| `api/internal/server/middleware.go` | Request ID, access log, panic recovery, logger |
| `api/cmd/api/main.go` | `api serve` |
| `web/vite.config.ts` | Plugins, `@` alias, proxy, env dir |
| `web/orval.config.ts` | Client + Zod generation |
| `web/src/auth/oidc.ts` | Shared `UserManager`, sign-in callback |
| `web/src/api/fetcher.ts` | Fetch mutator: bearer token, 401 → login, `ApiError` |
| `web/src/main.tsx` | Providers, router |
| `web/src/routes/__root.tsx` | Root layout, router context type |
| `web/src/routes/index.tsx` | Protected home page showing `/v1/me` |
| `README.md` | Prerequisites, first run, commands, troubleshooting |

---

### Task 1: Dev environment in WSL (manual, done by the developer)

This task needs `sudo` and GUI settings, so the developer runs it by hand. Later tasks run inside the WSL checkout.

**Files:** none in the repo. Edits `C:\Users\<user>\.wslconfig` and the WSL home directory.

- [ ] **Step 1: Give WSL more memory (needed for #2, harmless now)**

Edit `C:\Users\<user>\.wslconfig` on Windows so it contains:

```ini
[wsl2]
networkingMode=mirrored
memory=24GB
```

Then, in PowerShell: `wsl --shutdown`.

- [ ] **Step 2: Base packages**

In the Ubuntu-26.04 terminal:

```bash
sudo apt update
sudo apt install -y make git curl jq ca-certificates gh
```

- [ ] **Step 3: Go 1.27 (amd64)**

```bash
curl -LO https://go.dev/dl/go1.27.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.3.linux-amd64.tar.gz
rm go1.27.3.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.profile
source ~/.profile
go version
```

Expected: `go version go1.27.3 linux/amd64`

- [ ] **Step 4: Node 24 LTS**

```bash
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
source ~/.nvm/nvm.sh
nvm install 24
node -v
```

Expected: `v24.x.y`

- [ ] **Step 5: Docker in WSL**

Docker Desktop → Settings → Resources → WSL integration → enable `Ubuntu-26.04` → Apply & restart. Then in WSL:

```bash
docker version --format '{{.Server.Version}}'
docker compose version
```

Expected: both print a version, no permission error.

- [ ] **Step 6: Clone into the WSL filesystem**

```bash
gh auth login
git config --global user.name "<name>"
git config --global user.email "<email>"
mkdir -p ~/projects && cd ~/projects
gh repo clone alephc0rporation/Aleph0-Cloud-Ecosystem
cd Aleph0-Cloud-Ecosystem
git switch feat/3-app-skeleton
code .
```

Expected: VS Code opens with "WSL: Ubuntu-26.04" in the bottom-left corner. From now on, work only in this checkout and start Claude Code from this VS Code window.

---

### Task 2: Local services (PostgreSQL + Keycloak)

**Files:**
- Create: `.gitattributes`, `.env.example`, `Makefile`, `docker-compose.yml`, `deploy/postgres/init.sql`, `deploy/keycloak/realm-dev.json`

**Interfaces:**
- Produces: `make up` / `make down` / `make reset`; `.env` variables used by later tasks (`HTTP_ADDR`, `DATABASE_URL`, `OIDC_ISSUER`, `OIDC_AUDIENCE`, `VITE_OIDC_AUTHORITY`, `VITE_OIDC_CLIENT_ID`); DB user `api` / password `api` on database `api`.

- [ ] **Step 1: Line endings**

Create `.gitattributes`:

```gitattributes
# LF everywhere: Makefiles and shell scripts break with CRLF.
* text=auto eol=lf
```

- [ ] **Step 2: Environment template**

Create `.env.example`:

```dotenv
# Copy to .env (never committed). Dev-only values.
# No quotes and no `$` in values: the Makefile reads this file too.

# PostgreSQL superuser (docker compose only)
POSTGRES_PASSWORD=postgres

# Keycloak admin console (http://localhost:8081)
KC_BOOTSTRAP_ADMIN_USERNAME=admin
KC_BOOTSTRAP_ADMIN_PASSWORD=admin

# API
HTTP_ADDR=:8080
DATABASE_URL=postgres://api:api@localhost:5432/api?sslmode=disable
OIDC_ISSUER=http://localhost:8081/realms/aleph0
OIDC_AUDIENCE=api

# Web (only VITE_* variables reach the browser)
VITE_OIDC_AUTHORITY=http://localhost:8081/realms/aleph0
VITE_OIDC_CLIENT_ID=web
```

Then `cp .env.example .env`.

- [ ] **Step 3: Database init script**

Create `deploy/postgres/init.sql`:

```sql
-- Dev only. Runs once, when the data volume is empty.
-- Production uses Cloud SQL with its own users and secrets.
CREATE USER api WITH PASSWORD 'api';
CREATE DATABASE api OWNER api;

CREATE USER keycloak WITH PASSWORD 'keycloak';
CREATE DATABASE keycloak OWNER keycloak;

-- Only each database's owner may connect to it.
REVOKE CONNECT ON DATABASE api FROM PUBLIC;
REVOKE CONNECT ON DATABASE keycloak FROM PUBLIC;
```

- [ ] **Step 4: Keycloak realm**

Create `deploy/keycloak/realm-dev.json`:

```json
{
  "realm": "aleph0",
  "enabled": true,
  "sslRequired": "external",
  "registrationAllowed": false,
  "loginWithEmailAllowed": true,
  "clients": [
    {
      "clientId": "web",
      "name": "Aleph0 web console",
      "enabled": true,
      "publicClient": true,
      "standardFlowEnabled": true,
      "implicitFlowEnabled": false,
      "directAccessGrantsEnabled": false,
      "serviceAccountsEnabled": false,
      "redirectUris": ["http://localhost:5173/*"],
      "webOrigins": ["http://localhost:5173"],
      "attributes": {
        "pkce.code.challenge.method": "S256",
        "post.logout.redirect.uris": "http://localhost:5173/*"
      },
      "protocolMappers": [
        {
          "name": "api-audience",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-audience-mapper",
          "consentRequired": false,
          "config": {
            "included.client.audience": "api",
            "id.token.claim": "false",
            "access.token.claim": "true",
            "introspection.token.claim": "true"
          }
        }
      ]
    },
    {
      "clientId": "api",
      "name": "Aleph0 API (token audience only)",
      "enabled": true,
      "publicClient": false,
      "standardFlowEnabled": false,
      "implicitFlowEnabled": false,
      "directAccessGrantsEnabled": false,
      "serviceAccountsEnabled": false
    }
  ],
  "users": [
    {
      "username": "dev@aleph0.local",
      "email": "dev@aleph0.local",
      "emailVerified": true,
      "firstName": "Dev",
      "lastName": "User",
      "enabled": true,
      "credentials": [{ "type": "password", "value": "dev", "temporary": false }]
    }
  ]
}
```

- [ ] **Step 5: Pin image versions**

Find the latest PostgreSQL 18 and Keycloak 26 tags:

```bash
curl -s "https://hub.docker.com/v2/repositories/library/postgres/tags?page_size=100&name=18." \
  | jq -r '.results[].name' | grep -E '^18\.[0-9]+$' | sort -V | tail -1
curl -s "https://quay.io/api/v1/repository/keycloak/keycloak/tag/?limit=100&onlyActiveTags=true" \
  | jq -r '.tags[].name' | grep -E '^26\.[0-9]+\.[0-9]+$' | sort -V | tail -1
```

Use the two printed tags in the next step in place of `<POSTGRES_TAG>` and `<KEYCLOAK_TAG>`.

- [ ] **Step 6: Compose file**

Create `docker-compose.yml` (replace the two tags):

```yaml
name: aleph0

services:
  postgres:
    image: postgres:<POSTGRES_TAG>
    environment:
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set it in .env}
    ports:
      # Loopback only: the dev superuser password must not be reachable from the LAN.
      - "127.0.0.1:5432:5432"
    volumes:
      # PostgreSQL 18 images keep data under /var/lib/postgresql/18/docker.
      - pgdata:/var/lib/postgresql
      - ./deploy/postgres/init.sql:/docker-entrypoint-initdb.d/init.sql:ro
    healthcheck:
      # TCP on purpose: during init.sql the server listens on the Unix socket only,
      # so a socket check would report ready before the keycloak database exists.
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U postgres"]
      interval: 2s
      timeout: 5s
      retries: 30

  keycloak:
    image: quay.io/keycloak/keycloak:<KEYCLOAK_TAG>
    command: ["start-dev", "--import-realm"]
    environment:
      KC_DB: postgres
      KC_DB_URL: jdbc:postgresql://postgres:5432/keycloak
      KC_DB_USERNAME: keycloak
      KC_DB_PASSWORD: keycloak
      KC_HEALTH_ENABLED: "true"
      KC_BOOTSTRAP_ADMIN_USERNAME: ${KC_BOOTSTRAP_ADMIN_USERNAME:?set it in .env}
      KC_BOOTSTRAP_ADMIN_PASSWORD: ${KC_BOOTSTRAP_ADMIN_PASSWORD:?set it in .env}
    ports:
      - "127.0.0.1:8081:8080"
    volumes:
      - ./deploy/keycloak:/opt/keycloak/data/import:ro
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      # The image has no curl; bash's /dev/tcp hits the management port.
      test:
        - CMD
        - bash
        - -c
        - 'exec 3<>/dev/tcp/localhost/9000 && printf ''GET /health/ready HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n'' >&3 && grep -q ''200 OK'' <&3'
      interval: 5s
      timeout: 5s
      retries: 60
      start_period: 10s

volumes:
  pgdata:
```

- [ ] **Step 7: Makefile with the service targets**

Create `Makefile` (recipe lines start with a TAB):

```make
# Load .env when present and export it to every recipe.
-include .env
export

.PHONY: up down reset

up:
	docker compose up -d --wait

down:
	docker compose down

# Wipes the DB volume; Keycloak re-imports the realm on the next start.
reset:
	docker compose down -v
	$(MAKE) up
```

- [ ] **Step 8: Start and verify**

```bash
make up
docker compose ps --format '{{.Service}} {{.Health}}'
```

Expected: `keycloak healthy` and `postgres healthy`.

```bash
docker compose port postgres 5432
docker compose port keycloak 8080
```

Expected: `127.0.0.1:5432` and `127.0.0.1:8081`, nothing on `0.0.0.0`.

```bash
docker compose exec postgres psql -U postgres -Atc "select datname from pg_database where datname in ('api','keycloak') order by 1"
```

Expected: `api` and `keycloak`.

```bash
docker compose exec -e PGPASSWORD=api postgres psql -h localhost -U api -d keycloak -c 'select 1'
```

Expected: fails with `permission denied for database "keycloak"`.

```bash
curl -s localhost:8081/realms/aleph0/.well-known/openid-configuration | jq -r .issuer
```

Expected: `http://localhost:8081/realms/aleph0`

```bash
T=$(curl -s -d client_id=admin-cli -d username=admin -d password=admin -d grant_type=password \
  localhost:8081/realms/master/protocol/openid-connect/token | jq -r .access_token)
curl -s -H "Authorization: Bearer $T" localhost:8081/admin/realms/aleph0/clients | jq -r '.[].clientId' | grep -E '^(web|api)$'
curl -s -H "Authorization: Bearer $T" "localhost:8081/admin/realms/aleph0/users?email=dev@aleph0.local" | jq -r '.[].username'
```

Expected: `api`, `web`, then `dev@aleph0.local`.

- [ ] **Step 9: Reset works**

```bash
make reset && docker compose ps --format '{{.Service}} {{.Health}}'
```

Expected: both `healthy` again.

- [ ] **Step 10: Commit**

```bash
git add .gitattributes .env.example Makefile docker-compose.yml deploy/
git commit -m "feat: add local PostgreSQL and Keycloak with dev realm"
```

---

### Task 3: Go module and config

**Files:**
- Create: `api/go.mod`, `api/internal/config/config.go`
- Test: `api/internal/config/config_test.go`
- Modify: `Makefile`

**Interfaces:**
- Produces: `config.Config{HTTPAddr, DatabaseURL, OIDCIssuer, OIDCAudience string}`; `config.Load(getenv func(string) string) (config.Config, error)`.

- [ ] **Step 1: Create the module**

```bash
mkdir -p api && cd api
go mod init github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api
cd ..
```

- [ ] **Step 2: Write the failing test**

Create `api/internal/config/config_test.go`:

```go
package config_test

import (
	"strings"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	full := map[string]string{
		"DATABASE_URL":  "postgres://api:api@localhost:5432/api",
		"OIDC_ISSUER":   "http://localhost:8081/realms/aleph0",
		"OIDC_AUDIENCE": "api",
	}

	t.Run("defaults HTTP_ADDR", func(t *testing.T) {
		c, err := config.Load(env(full))
		if err != nil {
			t.Fatal(err)
		}
		want := config.Config{
			HTTPAddr:     ":8080",
			DatabaseURL:  full["DATABASE_URL"],
			OIDCIssuer:   full["OIDC_ISSUER"],
			OIDCAudience: "api",
		}
		if c != want {
			t.Fatalf("got %+v, want %+v", c, want)
		}
	})

	t.Run("HTTP_ADDR override", func(t *testing.T) {
		m := map[string]string{"HTTP_ADDR": ":9999"}
		for k, v := range full {
			m[k] = v
		}
		c, err := config.Load(env(m))
		if err != nil {
			t.Fatal(err)
		}
		if c.HTTPAddr != ":9999" {
			t.Fatalf("HTTPAddr = %q, want :9999", c.HTTPAddr)
		}
	})

	t.Run("lists every missing variable", func(t *testing.T) {
		_, err := config.Load(env(map[string]string{"OIDC_AUDIENCE": "api"}))
		if err == nil {
			t.Fatal("want error")
		}
		for _, name := range []string{"DATABASE_URL", "OIDC_ISSUER"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not mention %s", err, name)
			}
		}
		if strings.Contains(err.Error(), "OIDC_AUDIENCE") {
			t.Errorf("error %q mentions OIDC_AUDIENCE, which is set", err)
		}
	})
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd api && go test ./internal/config/`
Expected: FAIL, `undefined: config.Load` / `config.Config`.

- [ ] **Step 4: Implement**

Create `api/internal/config/config.go`:

```go
// Package config reads the API configuration from environment variables.
package config

import (
	"fmt"
	"strings"
)

type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	OIDCIssuer   string
	OIDCAudience string
}

// Load reads the configuration through getenv (os.Getenv outside tests)
// and reports every missing required variable at once.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		HTTPAddr:     getenv("HTTP_ADDR"),
		DatabaseURL:  getenv("DATABASE_URL"),
		OIDCIssuer:   getenv("OIDC_ISSUER"),
		OIDCAudience: getenv("OIDC_AUDIENCE"),
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}

	required := []struct{ name, value string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"OIDC_ISSUER", c.OIDCIssuer},
		{"OIDC_AUDIENCE", c.OIDCAudience},
	}
	var missing []string
	for _, r := range required {
		if r.value == "" {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return c, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `cd api && go test ./internal/config/`
Expected: `ok`

- [ ] **Step 6: Add the test target**

Append to `Makefile`:

```make
.PHONY: test
test:
	cd api && go vet ./... && go test ./...
```

Run: `make test` → Expected: `ok .../internal/config`.

- [ ] **Step 7: Commit**

```bash
git add api/go.mod api/internal/config Makefile
git commit -m "feat(api): add Go module and env config"
```

---

### Task 4: OpenAPI contract and generated server code

**Files:**
- Create: `api/openapi.yaml`, `api/oapi-codegen.yaml`, `api/internal/oapi/oapi.gen.go` (generated)
- Modify: `api/go.mod`, `api/go.sum`, `Makefile`

**Interfaces:**
- Produces (generated in package `oapi`, import path `.../api/internal/oapi`):
  - `StrictServerInterface` with `GetHealthz(ctx context.Context, request GetHealthzRequestObject) (GetHealthzResponseObject, error)` and `GetMe(ctx context.Context, request GetMeRequestObject) (GetMeResponseObject, error)`
  - response types `GetHealthz200JSONResponse`, `GetHealthz503JSONResponse` (underlying `Health{Status HealthStatus}`), `GetMe200JSONResponse` (underlying `User{Id, Email, Name string}`)
  - `NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface`
  - `HandlerWithOptions(si ServerInterface, options StdHTTPServerOptions) http.Handler`
  - `make generate` (Go part)

- [ ] **Step 1: Write the contract**

Create `api/openapi.yaml`:

```yaml
openapi: 3.0.3
info:
  title: Aleph0 Cloud API
  version: 0.1.0
security:
  - bearerAuth: []
paths:
  /healthz:
    get:
      operationId: getHealthz
      summary: Liveness and database check
      security: []
      responses:
        "200":
          description: The API and its database are reachable.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Health"
        "503":
          description: The database is unreachable.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Health"
  /v1/me:
    get:
      operationId: getMe
      summary: The user the access token belongs to
      responses:
        "200":
          description: The authenticated user.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
        "401":
          $ref: "#/components/responses/Unauthorized"
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  responses:
    Unauthorized:
      description: Missing or invalid access token.
      headers:
        WWW-Authenticate:
          schema:
            type: string
      content:
        application/problem+json:
          schema:
            $ref: "#/components/schemas/Problem"
  schemas:
    Health:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [ok, unavailable]
    User:
      type: object
      required: [id, email, name]
      properties:
        id:
          type: string
          description: Keycloak subject (the token's `sub` claim).
        email:
          type: string
        name:
          type: string
    Problem:
      description: RFC 9457 problem details.
      type: object
      required: [type, title, status]
      properties:
        type:
          type: string
          example: "about:blank"
        title:
          type: string
        status:
          type: integer
        detail:
          type: string
```

- [ ] **Step 2: Generator config**

Create `api/oapi-codegen.yaml`:

```yaml
package: oapi
output: internal/oapi/oapi.gen.go
generate:
  models: true
  std-http-server: true
  strict-server: true
```

- [ ] **Step 3: Pin the generator as a Go tool and generate**

```bash
cd api
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
mkdir -p internal/oapi
go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
go mod tidy
go build ./...
cd ..
```

Expected: `go build` succeeds; `api/go.mod` has a `tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen` line.

- [ ] **Step 4: Check the generated names this plan relies on**

```bash
grep -nE 'type (StrictServerInterface|GetHealthz200JSONResponse|GetHealthz503JSONResponse|GetMe200JSONResponse|StrictHTTPServerOptions|StdHTTPServerOptions) |func (NewStrictHandlerWithOptions|HandlerWithOptions)\(' api/internal/oapi/oapi.gen.go
```

Expected: one line for each of the 8 names. If a name differs, use the generated name in Tasks 6 and 7.

```bash
grep -nE '^\s+(BaseRouter|ErrorHandlerFunc) ' api/internal/oapi/oapi.gen.go
```

Expected: at least one line for each field; Task 7 sets both on `StdHTTPServerOptions`.

- [ ] **Step 5: Makefile target**

Append to `Makefile`:

```make
.PHONY: generate
generate:
	cd api && go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
```

Run: `make generate && git status --porcelain api/internal/oapi`
Expected: no output (generation is deterministic).

- [ ] **Step 6: Commit**

```bash
git add api/openapi.yaml api/oapi-codegen.yaml api/internal/oapi api/go.mod api/go.sum Makefile
git commit -m "feat(api): add OpenAPI contract and generated server code"
```

---

### Task 5: Problem details and JWT authentication

**Files:**
- Create: `api/internal/problem/problem.go`, `api/internal/auth/auth.go`
- Test: `api/internal/problem/problem_test.go`, `api/internal/auth/auth_test.go`

**Interfaces:**
- Produces:
  - `problem.Write(w http.ResponseWriter, status int, detail string)`
  - `auth.Claims{Subject, Email, Name string}` (JSON tags `sub`, `email`, `name`)
  - `auth.NewVerifier(issuer, audience string, keySet oidc.KeySet) *auth.Verifier`
  - `auth.NewKeycloakVerifier(issuer, audience string) *auth.Verifier`
  - `(*auth.Verifier).Verify(ctx context.Context, rawToken string) (auth.Claims, error)`
  - `(*auth.Verifier).Middleware(next http.Handler) http.Handler`
  - `auth.WithClaims(ctx context.Context, c auth.Claims) context.Context`
  - `auth.FromContext(ctx context.Context) (auth.Claims, bool)`

- [ ] **Step 1: Failing test for problem.Write**

Create `api/internal/problem/problem_test.go`:

```go
package problem_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusUnauthorized, "missing bearer token")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"type":   "about:blank",
		"title":  "Unauthorized",
		"status": float64(401),
		"detail": "missing bearer token",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestWriteOmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusInternalServerError, "")

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["detail"]; ok {
		t.Fatalf("detail present: %v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/problem/`
Expected: FAIL, `undefined: problem.Write`.

- [ ] **Step 3: Implement problem.Write**

Create `api/internal/problem/problem.go`:

```go
// Package problem writes RFC 9457 problem details responses.
package problem

import (
	"encoding/json"
	"net/http"
)

type body struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Write sends a problem details response. detail is shown to the client, so it must not leak internals.
func Write(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}
```

- [ ] **Step 4: Run the tests**

Run: `cd api && go test ./internal/problem/`
Expected: `ok`

- [ ] **Step 5: Add the OIDC library**

```bash
cd api && go get github.com/coreos/go-oidc/v3/oidc@latest && cd ..
```

- [ ] **Step 6: Failing test for the auth middleware**

Create `api/internal/auth/auth_test.go`:

```go
package auth_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
)

const issuer = "http://localhost:8081/realms/aleph0"

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func validClaims() map[string]any {
	return map[string]any{
		"iss":   issuer,
		"aud":   []string{"api", "account"},
		"sub":   "user-1",
		"email": "dev@aleph0.local",
		"name":  "Dev User",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

func with(c map[string]any, k string, v any) map[string]any {
	c[k] = v
	return c
}

func TestMiddleware(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v := auth.NewVerifier(issuer, "api", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}})

	var seen auth.Claims
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = auth.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"valid token", "Bearer " + sign(t, key, validClaims()), http.StatusNoContent},
		{"lowercase scheme", "bearer " + sign(t, key, validClaims()), http.StatusNoContent},
		{"no header", "", http.StatusUnauthorized},
		{"basic scheme", "Basic ZGV2OmRldg==", http.StatusUnauthorized},
		{"empty token", "Bearer ", http.StatusUnauthorized},
		{"garbage token", "Bearer not-a-jwt", http.StatusUnauthorized},
		{"wrong audience", "Bearer " + sign(t, key, with(validClaims(), "aud", "web")), http.StatusUnauthorized},
		{"wrong issuer", "Bearer " + sign(t, key, with(validClaims(), "iss", "http://evil.example/realms/aleph0")), http.StatusUnauthorized},
		{"expired", "Bearer " + sign(t, key, with(validClaims(), "exp", time.Now().Add(-time.Minute).Unix())), http.StatusUnauthorized},
		{"wrong signing key", "Bearer " + sign(t, otherKey, validClaims()), http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen = auth.Claims{}
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
					t.Errorf("Content-Type = %q", got)
				}
				return
			}
			want := auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}
			if seen != want {
				t.Errorf("claims = %+v, want %+v", seen, want)
			}
		})
	}
}

func TestFromContextEmpty(t *testing.T) {
	if _, ok := auth.FromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("want ok = false without claims")
	}
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `cd api && go mod tidy && go test ./internal/auth/`
Expected: FAIL, `undefined: auth.NewVerifier` (and the other `auth.` names).

- [ ] **Step 8: Implement auth**

Create `api/internal/auth/auth.go`:

```go
// Package auth verifies Keycloak access tokens and carries the caller's identity in the request context.
package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
)

// Claims is the part of the access token the API uses.
type Claims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

type Verifier struct {
	v *oidc.IDTokenVerifier
}

// NewVerifier checks signature (RS256, keys from keySet), iss, exp, and that aud contains audience.
// ID tokens are rejected too: Keycloak issues them with aud = the client ID, never "api".
func NewVerifier(issuer, audience string, keySet oidc.KeySet) *Verifier {
	return &Verifier{oidc.NewVerifier(issuer, keySet, &oidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: []string{oidc.RS256},
	})}
}

// NewKeycloakVerifier fetches Keycloak's signing keys on first use and again when a token
// carries an unknown key ID, so the API can start while Keycloak is down.
func NewKeycloakVerifier(issuer, audience string) *Verifier {
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 10 * time.Second})
	return NewVerifier(issuer, audience, oidc.NewRemoteKeySet(ctx, issuer+"/protocol/openid-connect/certs"))
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	tok, err := v.v.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return Claims{}, err
	}
	return c, nil
}

// Middleware answers 401 unless the request carries a valid bearer token, and stores its claims in the context.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w, "missing bearer token")
			return
		}
		c, err := v.Verify(r.Context(), raw)
		if err != nil {
			slog.InfoContext(r.Context(), "access token rejected", "error", err)
			unauthorized(w, "invalid access token")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), c)))
	})
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, detail string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	problem.Write(w, http.StatusUnauthorized, detail)
}

type ctxKey struct{}

func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}
```

- [ ] **Step 9: Run the tests**

Run: `cd api && go mod tidy && go test ./internal/auth/ ./internal/problem/ -v`
Expected: PASS for the 10 subtests of `TestMiddleware`, `TestFromContextEmpty`, `TestWrite` and `TestWriteOmitsEmptyDetail`.

- [ ] **Step 10: Commit**

```bash
git add api/internal/problem api/internal/auth api/go.mod api/go.sum
git commit -m "feat(api): add problem details and Keycloak JWT middleware"
```

---

### Task 6: Handlers

**Files:**
- Create: `api/internal/handlers/handlers.go`
- Test: `api/internal/handlers/handlers_test.go`

**Interfaces:**
- Consumes: `oapi.StrictServerInterface` and response types (Task 4); `auth.WithClaims`, `auth.FromContext`, `auth.Claims` (Task 5).
- Produces: `handlers.Handlers{Ping func(context.Context) error}`, implementing `oapi.StrictServerInterface`.

- [ ] **Step 1: Write the failing test**

Create `api/internal/handlers/handlers_test.go`:

```go
package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/oapi"
)

func TestGetHealthz(t *testing.T) {
	ok := &handlers.Handlers{Ping: func(context.Context) error { return nil }}
	res, err := ok.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, isOK := res.(oapi.GetHealthz200JSONResponse); !isOK || got.Status != "ok" {
		t.Fatalf("got %#v, want 200 ok", res)
	}

	down := &handlers.Handlers{Ping: func(context.Context) error { return errors.New("connection refused") }}
	res, err = down.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, isDown := res.(oapi.GetHealthz503JSONResponse); !isDown || got.Status != "unavailable" {
		t.Fatalf("got %#v, want 503 unavailable", res)
	}
}

func TestGetHealthzBoundsThePing(t *testing.T) {
	h := &handlers.Handlers{Ping: func(ctx context.Context) error {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Error("ping context has no deadline")
		}
		return nil
	}}
	if _, err := h.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{}); err != nil {
		t.Fatal(err)
	}
}

func TestGetMe(t *testing.T) {
	h := &handlers.Handlers{}
	ctx := auth.WithClaims(context.Background(), auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"})

	res, err := h.GetMe(ctx, oapi.GetMeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	want := oapi.GetMe200JSONResponse{Id: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}
	if res != want {
		t.Fatalf("got %#v, want %#v", res, want)
	}

	if _, err := h.GetMe(context.Background(), oapi.GetMeRequestObject{}); err == nil {
		t.Fatal("want error without claims in context")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/handlers/`
Expected: FAIL, `undefined: handlers.Handlers`.

- [ ] **Step 3: Implement**

Create `api/internal/handlers/handlers.go`:

```go
// Package handlers implements the operations defined in api/openapi.yaml.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/oapi"
)

type Handlers struct {
	// Ping checks the database connection.
	Ping func(context.Context) error
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

func (h *Handlers) GetHealthz(ctx context.Context, _ oapi.GetHealthzRequestObject) (oapi.GetHealthzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.Ping(ctx); err != nil {
		slog.WarnContext(ctx, "database ping failed", "error", err)
		return oapi.GetHealthz503JSONResponse{Status: "unavailable"}, nil
	}
	return oapi.GetHealthz200JSONResponse{Status: "ok"}, nil
}

func (h *Handlers) GetMe(ctx context.Context, _ oapi.GetMeRequestObject) (oapi.GetMeResponseObject, error) {
	c, ok := auth.FromContext(ctx)
	if !ok {
		return nil, errors.New("no claims in context: /v1 route mounted without auth middleware")
	}
	return oapi.GetMe200JSONResponse{Id: c.Subject, Email: c.Email, Name: c.Name}, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `cd api && go test ./internal/handlers/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add api/internal/handlers
git commit -m "feat(api): add healthz and me handlers"
```

---

### Task 7: HTTP server, routing and middleware

**Files:**
- Create: `api/internal/server/server.go`, `api/internal/server/middleware.go`
- Test: `api/internal/server/server_test.go`

**Interfaces:**
- Consumes: `oapi.StrictServerInterface`, `oapi.NewStrictHandlerWithOptions`, `oapi.StrictHTTPServerOptions`, `oapi.HandlerWithOptions`, `oapi.StdHTTPServerOptions` (Task 4); `problem.Write` (Task 5); `handlers.Handlers` and `auth.WithClaims` (tests only).
- Produces:
  - `server.Handler(api oapi.StrictServerInterface, requireAuth func(http.Handler) http.Handler) http.Handler`
  - `server.NewLogger(w io.Writer) *slog.Logger`
  - `server.Run(ctx context.Context, addr string, h http.Handler) error`

- [ ] **Step 1: Write the failing test**

Create `api/internal/server/server_test.go`:

```go
package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/server"
)

var claims = auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}

func allow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
	})
}

func deny(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
}

func pingOK(context.Context) error { return nil }

func do(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthzSkipsAuth(t *testing.T) {
	rec := do(t, server.Handler(&handlers.Handlers{Ping: pingOK}, deny), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestV1RequiresAuth(t *testing.T) {
	h := server.Handler(&handlers.Handlers{Ping: pingOK}, deny)
	for _, path := range []string{"/v1/me", "/v1/does-not-exist"} {
		if rec := do(t, h, path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, rec.Code)
		}
	}
}

func TestMe(t *testing.T) {
	rec := do(t, server.Handler(&handlers.Handlers{Ping: pingOK}, allow), "/v1/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "user-1" || got["email"] != "dev@aleph0.local" || got["name"] != "Dev User" {
		t.Fatalf("body = %v", got)
	}
}

func TestUnknownPathIsProblem404(t *testing.T) {
	h := server.Handler(&handlers.Handlers{Ping: pingOK}, allow)
	for _, path := range []string{"/nope", "/v1/does-not-exist"} {
		rec := do(t, h, path)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status = %d, Content-Type = %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestPanicIsProblem500AndLoggedWithRequestID(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(server.NewLogger(&logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := &handlers.Handlers{Ping: func(context.Context) error { panic("boom") }}
	rec := do(t, server.Handler(boom, allow), "/healthz")

	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status = %d, Content-Type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	id := rec.Header().Get("X-Request-Id")
	var sawPanic, sawAccess bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %s", line)
		}
		if entry["request_id"] != id {
			t.Errorf("log line without request_id %s: %s", id, line)
		}
		switch entry["msg"] {
		case "panic":
			sawPanic = true
		case "request":
			sawAccess = entry["status"] == float64(500)
		}
	}
	if !sawPanic || !sawAccess {
		t.Fatalf("panic logged = %v, access log with 500 = %v; logs:\n%s", sawPanic, sawAccess, logs.String())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/server/`
Expected: FAIL, `undefined: server.Handler`.

- [ ] **Step 3: Middleware**

Create `api/internal/server/middleware.go`:

```go
package server

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
)

type requestIDKey struct{}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// NewLogger returns a JSON logger that adds request_id to every record logged with a request context.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(contextHandler{slog.NewJSONHandler(w, nil)})
}

type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { // deliberate abort, let net/http handle it
				panic(v)
			}
			slog.ErrorContext(r.Context(), "panic", "value", v, "stack", string(debug.Stack()))
			problem.Write(w, http.StatusInternalServerError, "")
		}()
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Routing and server lifecycle**

Create `api/internal/server/server.go`:

```go
// Package server wires the HTTP stack: routing, middleware and the http.Server lifecycle.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/oapi"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
)

// Handler builds the root handler. Everything under /v1/ goes through requireAuth,
// so a new /v1 endpoint is protected unless it is deliberately mounted elsewhere.
func Handler(api oapi.StrictServerInterface, requireAuth func(http.Handler) http.Handler) http.Handler {
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		problem.Write(w, http.StatusBadRequest, err.Error())
	}
	// The generated routes are added to base, so unknown paths and methods
	// get a problem+json 404 instead of net/http's plain-text answers.
	base := http.NewServeMux()
	base.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, http.StatusNotFound, "")
	})
	generated := oapi.HandlerWithOptions(
		oapi.NewStrictHandlerWithOptions(api, nil, oapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: badRequest,
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				slog.ErrorContext(r.Context(), "handler failed", "error", err)
				problem.Write(w, http.StatusInternalServerError, "")
			},
		}),
		oapi.StdHTTPServerOptions{BaseRouter: base, ErrorHandlerFunc: badRequest},
	)

	mux := http.NewServeMux()
	mux.Handle("/v1/", requireAuth(generated))
	mux.Handle("/", generated)
	return requestID(accessLog(recoverPanic(mux)))
}

// Run serves h on addr until ctx is cancelled, then gives in-flight requests 10 s to finish.
func Run(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 5: Run the tests**

Run: `cd api && go test ./internal/server/ -v`
Expected: all 5 tests PASS.

- [ ] **Step 6: Run the whole Go suite**

Run: `make test`
Expected: `go vet` clean, every package `ok`.

- [ ] **Step 7: Commit**

```bash
git add api/internal/server
git commit -m "feat(api): add HTTP server with routing, auth wiring and middleware"
```

---

### Task 8: `api serve` command

**Files:**
- Create: `api/cmd/api/main.go`
- Modify: `Makefile`, `api/go.mod`, `api/go.sum`

**Interfaces:**
- Consumes: `config.Load` (Task 3), `auth.NewKeycloakVerifier` (Task 5), `handlers.Handlers` (Task 6), `server.Handler`, `server.NewLogger`, `server.Run` (Task 7).
- Produces: `make dev-api`; API on `:8080`.

- [ ] **Step 1: Add pgx**

```bash
cd api && go get github.com/jackc/pgx/v5@latest && cd ..
```

- [ ] **Step 2: Write main**

Create `api/cmd/api/main.go`:

```go
// Command api is the Aleph0 Cloud backend.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/config"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/server"
)

const usage = "usage: api serve"

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	slog.SetDefault(server.NewLogger(os.Stdout))
	if err := serve(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The pool connects lazily: the API starts while the DB is down and /healthz reports it.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database config: %w", err)
	}
	defer pool.Close()

	verifier := auth.NewKeycloakVerifier(cfg.OIDCIssuer, cfg.OIDCAudience)
	h := server.Handler(&handlers.Handlers{Ping: pool.Ping}, verifier.Middleware)

	slog.Info("listening", "addr", cfg.HTTPAddr)
	return server.Run(ctx, cfg.HTTPAddr, h)
}
```

- [ ] **Step 3: Makefile target**

Append to `Makefile`:

```make
.PHONY: dev-api
dev-api:
	cd api && go run ./cmd/api serve
```

Run: `cd api && go mod tidy && go build ./... && cd ..`
Expected: builds.

- [ ] **Step 4: Verify usage and config errors**

Use a built binary: `go run` exits with 1 whenever the program fails, which hides the real exit code.

```bash
cd api
go build -o /tmp/aleph0-api ./cmd/api
/tmp/aleph0-api; echo "exit=$?"
env -i /tmp/aleph0-api serve; echo "exit=$?"
rm /tmp/aleph0-api
cd ..
```

Expected: first prints `usage: api serve` and `exit=2`; second logs `missing required environment variables: DATABASE_URL, OIDC_ISSUER, OIDC_AUDIENCE` and prints `exit=1`.

- [ ] **Step 5: Verify against the running stack**

Terminal 1: `make up && make dev-api` → Expected: a JSON log line with `"msg":"listening"`.

Terminal 2:

```bash
curl -si localhost:8080/healthz
```

Expected: `HTTP/1.1 200`, `X-Request-Id` header, body `{"status":"ok"}`.

```bash
curl -si localhost:8080/v1/me
```

Expected: `HTTP/1.1 401`, `Www-Authenticate: Bearer`, `Content-Type: application/problem+json`, body with `"detail":"missing bearer token"`.

```bash
docker compose stop postgres
curl -si localhost:8080/healthz
docker compose start postgres
```

Expected: `HTTP/1.1 503` with `{"status":"unavailable"}`, within about 2 s.

Back in terminal 1, press Ctrl+C. Expected: the process exits without a panic or stack trace.

- [ ] **Step 6: Commit**

```bash
git add api/cmd api/go.mod api/go.sum Makefile
git commit -m "feat(api): add api serve command"
```

---

### Task 9: Web scaffold (Vite, Tailwind, shadcn/ui, TanStack Router)

**Files:**
- Create: `web/` (Vite template), `web/src/routes/__root.tsx`, `web/src/routes/index.tsx`, `web/src/routeTree.gen.ts` (generated), `web/components.json`, `web/src/components/ui/button.tsx`, `web/src/components/ui/card.tsx`, `web/src/lib/utils.ts`
- Modify: `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.app.json`, `web/src/main.tsx`, `web/src/index.css`, `web/index.html`, `web/eslint.config.js`, `web/package.json`, `Makefile`
- Delete: `web/src/App.tsx`, `web/src/App.css`, `web/src/assets/`, `web/public/vite.svg`

**Interfaces:**
- Produces: `@/` import alias to `web/src/`; `Button` from `@/components/ui/button`; `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` from `@/components/ui/card`; npm scripts `dev`, `build`, `lint`, `typecheck`; `make dev-web`.

- [ ] **Step 1: Create the Vite app**

```bash
npm create vite@latest web -- --template react-ts
```

If asked whether to install and start now, answer **No**. Then:

```bash
cd web
npm install
rm -rf src/App.tsx src/App.css src/assets public/vite.svg
```

- [ ] **Step 2: Install Tailwind, router and type helpers**

```bash
npm install tailwindcss @tailwindcss/vite @tanstack/react-router
npm install -D @tanstack/router-plugin @types/node
```

- [ ] **Step 3: Path alias in both tsconfig files**

In `web/tsconfig.json`, add next to `files` and `references`:

```json
  "compilerOptions": {
    "paths": { "@/*": ["./src/*"] }
  }
```

In `web/tsconfig.app.json`, add inside the existing `compilerOptions`:

```json
    "paths": { "@/*": ["./src/*"] }
```

- [ ] **Step 4: Vite config**

Replace `web/vite.config.ts`:

```ts
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  // One .env for the whole repo; only VITE_* variables reach the browser.
  envDir: '..',
  // The router plugin must run before the React plugin.
  plugins: [tanstackRouter({ target: 'react', autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  server: {
    port: 5173,
    // Keycloak only accepts redirects to :5173, so fail instead of moving to another port.
    strictPort: true,
    proxy: { '/v1': 'http://localhost:8080' },
  },
})
```

If the template uses `@vitejs/plugin-react-swc`, keep that import instead of `@vitejs/plugin-react`.

- [ ] **Step 5: Tailwind entry and shadcn/ui**

Replace `web/src/index.css` with:

```css
@import "tailwindcss";
```

Then:

```bash
npx shadcn@latest init
```

Choose the **Neutral** base color when asked. It writes `components.json`, `src/lib/utils.ts` and theme variables into `src/index.css`. Then:

```bash
npx shadcn@latest add button card
```

Expected: `src/components/ui/button.tsx` and `src/components/ui/card.tsx` exist.

- [ ] **Step 6: Routes**

Create `web/src/routes/__root.tsx`:

```tsx
import { Outlet, createRootRoute } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: () => (
    <main className="min-h-svh bg-background text-foreground">
      <Outlet />
    </main>
  ),
})
```

Create `web/src/routes/index.tsx`:

```tsx
import { createFileRoute } from '@tanstack/react-router'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/')({
  component: Home,
})

function Home() {
  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Aleph0 Cloud</CardTitle>
        </CardHeader>
      </Card>
    </div>
  )
}
```

- [ ] **Step 7: Entry point**

Replace `web/src/main.tsx`:

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import './index.css'

const router = createRouter({ routeTree })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
```

In `web/index.html`, set `<title>Aleph0 Cloud</title>` and remove the `<link rel="icon" ...vite.svg>` line.

- [ ] **Step 8: Lint ignores and scripts**

In `web/eslint.config.js`, extend the existing global ignore list (it holds `'dist'`) so it reads:

```js
globalIgnores(['dist', 'src/routeTree.gen.ts', 'src/api/gen', 'src/components/ui'])
```

If the file uses an `ignores: ['dist']` entry instead, add the same three paths there. Generated and vendored shadcn files are not linted.

In `web/package.json` `scripts`, add:

```json
    "typecheck": "tsc -b"
```

- [ ] **Step 9: Generate the route tree and check**

```bash
npx vite build
npm run lint
npm run typecheck
cd ..
```

Expected: build succeeds and writes `src/routeTree.gen.ts`; lint and typecheck report no errors.

- [ ] **Step 10: Makefile targets**

Append to `Makefile`:

```make
# Reinstall when the lock file changes.
web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@

.PHONY: dev-web
dev-web: web/node_modules
	cd web && npm run dev
```

Change the `test` target to:

```make
test: web/node_modules
	cd api && go vet ./... && go test ./...
	cd web && npm run lint && npm run typecheck
```

Run: `make test` → Expected: Go tests pass, lint and typecheck clean.
Run: `make dev-web`, open `http://localhost:5173` from the Windows browser → Expected: a centered card titled "Aleph0 Cloud". Stop with Ctrl+C.

- [ ] **Step 11: Commit**

```bash
git add web Makefile
git commit -m "feat(web): scaffold Vite app with Tailwind, shadcn/ui and TanStack Router"
```

---

### Task 10: Login, generated API client and the profile page

**Files:**
- Create: `web/orval.config.ts`, `web/src/auth/oidc.ts`, `web/src/api/fetcher.ts`, `web/src/api/gen/api.ts` (generated), `web/src/api/gen/api.zod.ts` (generated)
- Test: `web/src/api/fetcher.test.ts`
- Modify: `web/src/main.tsx`, `web/src/routes/__root.tsx`, `web/src/routes/index.tsx`, `web/package.json`, `Makefile`

**Interfaces:**
- Consumes: `api/openapi.yaml` (Task 4); `Button`, `Card*` (Task 9); the running API (Task 8) through the Vite proxy.
- Produces:
  - `userManager: UserManager` and `onSigninCallback(): void` from `@/auth/oidc`
  - `apiFetch<T>(url: string, init?: RequestInit): Promise<T>` and `class ApiError extends Error { status: number; problem?: Problem }` from `@/api/fetcher`
  - `useGetMe()` hook from `@/api/gen/api` (generated)
  - `RouterContext { auth: AuthContextProps }` from `@/routes/__root`

- [ ] **Step 1: Install**

```bash
cd web
npm install oidc-client-ts react-oidc-context @tanstack/react-query zod
npm install -D orval vitest
```

- [ ] **Step 2: Shared OIDC client**

Create `web/src/auth/oidc.ts`:

```ts
import { UserManager } from 'oidc-client-ts'

// One instance shared by React (AuthProvider) and the fetch mutator. Tokens live in sessionStorage.
export const userManager = new UserManager({
  authority: import.meta.env.VITE_OIDC_AUTHORITY,
  client_id: import.meta.env.VITE_OIDC_CLIENT_ID,
  redirect_uri: `${window.location.origin}/`,
  post_logout_redirect_uri: `${window.location.origin}/`,
  scope: 'openid profile email',
})

// Removes ?code=&state= from the address bar after Keycloak redirects back.
export function onSigninCallback() {
  window.history.replaceState({}, document.title, window.location.pathname)
}
```

- [ ] **Step 3: Failing test for the fetch mutator**

Add to `web/package.json` `scripts`:

```json
    "test": "vitest run"
```

Create `web/src/api/fetcher.test.ts`:

```ts
import { beforeEach, expect, it, vi } from 'vitest'
import { userManager } from '@/auth/oidc'
import { ApiError, apiFetch } from './fetcher'

vi.mock('@/auth/oidc', () => ({
  userManager: { getUser: vi.fn(), signinRedirect: vi.fn() },
}))

const fetchMock = vi.fn()

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('fetch', fetchMock)
})

it('sends the access token and returns the JSON body', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue({ access_token: 'tok' } as never)
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ id: 'user-1' }), { status: 200 }))

  await expect(apiFetch('/v1/me', { method: 'GET' })).resolves.toEqual({ id: 'user-1' })

  const [url, init] = fetchMock.mock.calls[0]
  expect(url).toBe('/v1/me')
  expect(init.method).toBe('GET')
  expect(new Headers(init.headers).get('Authorization')).toBe('Bearer tok')
})

it('sends no Authorization header without a session', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue(null)
  fetchMock.mockResolvedValue(new Response('{}', { status: 200 }))

  await apiFetch('/v1/me')

  expect(new Headers(fetchMock.mock.calls[0][1].headers).has('Authorization')).toBe(false)
})

it.each([
  ['no session', null],
  ['an expired session', { access_token: 'old', expired: true }],
])('starts the login redirect on 401 with %s', async (_, user) => {
  vi.mocked(userManager.getUser).mockResolvedValue(user as never)
  fetchMock.mockResolvedValue(new Response(null, { status: 401 }))

  await expect(apiFetch('/v1/me')).rejects.toBeInstanceOf(ApiError)
  expect(userManager.signinRedirect).toHaveBeenCalledOnce()
})

// Keycloak still has a session, so a redirect would come straight back and loop forever.
it('does not redirect when the API rejects a fresh token', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue({ access_token: 'tok', expired: false } as never)
  fetchMock.mockResolvedValue(new Response(null, { status: 401 }))

  const err = await apiFetch('/v1/me').catch((e: unknown) => e)
  expect((err as ApiError).status).toBe(401)
  expect(userManager.signinRedirect).not.toHaveBeenCalled()
})

it('throws the problem details on other errors', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue(null)
  const body = { type: 'about:blank', title: 'Internal Server Error', status: 500 }
  fetchMock.mockResolvedValue(new Response(JSON.stringify(body), { status: 500 }))

  const err = await apiFetch('/v1/me').catch((e: unknown) => e)
  expect(err).toBeInstanceOf(ApiError)
  expect((err as ApiError).status).toBe(500)
  expect((err as ApiError).problem).toEqual(body)
  expect(userManager.signinRedirect).not.toHaveBeenCalled()
})
```

- [ ] **Step 4: Run it to verify it fails**

Run: `npm test`
Expected: FAIL, cannot resolve `./fetcher`.

- [ ] **Step 5: Implement the mutator**

Create `web/src/api/fetcher.ts`:

```ts
import { userManager } from '@/auth/oidc'
import type { Problem } from './gen/api'

export class ApiError extends Error {
  readonly status: number
  readonly problem?: Problem

  constructor(status: number, problem?: Problem) {
    super(problem?.detail || problem?.title || `HTTP ${status}`)
    this.status = status
    this.problem = problem
  }
}

// Mutator used by the orval-generated client: adds the access token and turns
// non-2xx responses into ApiError.
export async function apiFetch<T>(url: string, init: RequestInit = {}): Promise<T> {
  const user = await userManager.getUser()
  const headers = new Headers(init.headers)
  if (user?.access_token) headers.set('Authorization', `Bearer ${user.access_token}`)

  const res = await fetch(url, { ...init, headers })
  // Log in again only without a usable session. A 401 for a fresh token means the API rejects
  // it (wrong aud or iss), and a redirect would bounce through Keycloak forever.
  if (res.status === 401 && (!user || user.expired)) {
    await userManager.signinRedirect()
    throw new ApiError(401)
  }
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined))
  }
  return (res.status === 204 ? undefined : await res.json()) as T
}
```

`./gen/api` is generated in Step 7. Vitest strips type-only imports, so the tests already pass; `tsc` needs Step 7.

- [ ] **Step 6: Run the tests**

Run: `npm test`
Expected: 6 tests PASS.

- [ ] **Step 7: Generate the API client**

Create `web/orval.config.ts`:

```ts
import { defineConfig } from 'orval'

export default defineConfig({
  api: {
    input: '../api/openapi.yaml',
    output: {
      mode: 'single',
      target: 'src/api/gen/api.ts',
      client: 'react-query',
      httpClient: 'fetch',
      override: {
        mutator: { path: 'src/api/fetcher.ts', name: 'apiFetch' },
        // Hooks return the response body itself, not { data, status, headers }.
        fetch: { includeHttpResponseReturnType: false },
      },
    },
  },
  apiZod: {
    input: '../api/openapi.yaml',
    output: {
      mode: 'single',
      target: 'src/api/gen/api.zod.ts',
      client: 'zod',
    },
  },
})
```

Add to `web/package.json` `scripts`:

```json
    "generate": "orval"
```

Run:

```bash
npm run generate
grep -n "export function useGetMe\|export const useGetMe" src/api/gen/api.ts
grep -n "apiFetch<User>" src/api/gen/api.ts
```

Expected: both greps print a line. If the second one prints nothing, the generated `getMe` returns a wrapper type instead of `User`: check the installed orval version's docs for the option that returns the body only, and fix `orval.config.ts` before going on.

- [ ] **Step 8: Router context with auth**

Replace `web/src/routes/__root.tsx`:

```tsx
import { Outlet, createRootRouteWithContext } from '@tanstack/react-router'
import type { AuthContextProps } from 'react-oidc-context'

export interface RouterContext {
  auth: AuthContextProps
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <main className="min-h-svh bg-background text-foreground">
      <Outlet />
    </main>
  ),
})
```

- [ ] **Step 9: Providers**

Replace `web/src/main.tsx`:

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { AuthProvider, useAuth } from 'react-oidc-context'
import { ApiError } from '@/api/fetcher'
import { onSigninCallback, userManager } from '@/auth/oidc'
import { routeTree } from './routeTree.gen'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx answer does not change on retry; only network errors and 5xx are retried.
      retry: (failures, err) => !(err instanceof ApiError && err.status < 500) && failures < 3,
    },
  },
})
const router = createRouter({ routeTree, context: { auth: undefined! } })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

function App() {
  const auth = useAuth()
  // Wait for the stored session or the sign-in callback before any route decides.
  if (auth.isLoading) return null
  // A failed callback would otherwise redirect to Keycloak and back in a loop.
  if (auth.error) {
    return (
      <p role="alert" className="p-4 text-destructive">
        Sign-in failed: {auth.error.message}
      </p>
    )
  }
  return <RouterProvider router={router} context={{ auth }} />
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider userManager={userManager} onSigninCallback={onSigninCallback}>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </AuthProvider>
  </StrictMode>,
)
```

- [ ] **Step 10: Protected home page**

Replace `web/src/routes/index.tsx`:

```tsx
import { createFileRoute } from '@tanstack/react-router'
import { useAuth } from 'react-oidc-context'
import { useGetMe } from '@/api/gen/api'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

export const Route = createFileRoute('/')({
  beforeLoad: async ({ context }) => {
    if (!context.auth.isAuthenticated) await context.auth.signinRedirect()
  },
  component: Home,
})

function Home() {
  const auth = useAuth()
  // The browser is already on its way to Keycloak.
  if (!auth.isAuthenticated) return null

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Aleph0 Cloud</CardTitle>
          <CardDescription>Signed in</CardDescription>
        </CardHeader>
        <CardContent>
          <Profile />
        </CardContent>
        <CardFooter>
          <Button variant="outline" onClick={() => void auth.signoutRedirect()}>
            Log out
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}

function Profile() {
  const me = useGetMe()
  if (me.isPending) return <p className="text-muted-foreground">Loading…</p>
  if (me.isError) {
    return (
      <p role="alert" className="text-destructive">
        Could not load your profile.
      </p>
    )
  }
  return (
    <dl className="grid gap-1">
      <dt className="text-sm text-muted-foreground">Name</dt>
      <dd>{me.data.name}</dd>
      <dt className="text-sm text-muted-foreground">Email</dt>
      <dd>{me.data.email}</dd>
    </dl>
  )
}
```

- [ ] **Step 11: Check**

```bash
npm run lint && npm run typecheck && npm test
cd ..
```

Expected: no lint or type errors, 6 tests PASS.

- [ ] **Step 12: Makefile**

Change the `generate` target to:

```make
generate: web/node_modules
	cd api && go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
	cd web && npm run generate
```

Change the web line of the `test` target to:

```make
	cd web && npm run lint && npm run typecheck && npm test
```

Add:

```make
.PHONY: dev
# Ctrl+C stops both.
dev: web/node_modules
	$(MAKE) -j2 dev-api dev-web
```

Run: `make generate && git status --porcelain` → Expected: no changes to generated files (other files from this task may still show as untracked/modified until committed).

- [ ] **Step 13: End-to-end in the browser**

```bash
make up
make dev
```

In the Windows browser, open `http://localhost:5173`. Expected:

1. Redirect to the Keycloak login page of realm `aleph0`.
2. Log in as `dev@aleph0.local` / `dev` → back on `http://localhost:5173/` with no `code` or `state` in the address bar.
3. The card shows Name "Dev User" and Email "dev@aleph0.local". The API log in the terminal shows `"path":"/v1/me","status":200`.
4. **Log out** → Keycloak's login page appears again.

Stop with Ctrl+C.

- [ ] **Step 14: Commit**

```bash
git add web Makefile
git commit -m "feat(web): add Keycloak login, generated API client and profile page"
```

---

### Task 11: README and final acceptance

**Files:**
- Create: `README.md`
- Already present: `docs/architecture.md` (committed with this plan; only check it still matches)

**Interfaces:**
- Consumes: every `make` target from Tasks 2–10.

- [ ] **Step 1: Write the README**

Create `README.md`:

````markdown
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
````

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with setup, commands and troubleshooting"
```

- [ ] **Step 3: Clean-checkout check**

A clone holds only committed files, so nothing installed or generated locally can hide a gap. No containers are needed: the tests use fakes.

```bash
rm -rf /tmp/accept && git clone ~/projects/Aleph0-Cloud-Ecosystem /tmp/accept && cd /tmp/accept
make test
make generate && git status --porcelain
cd ~/projects/Aleph0-Cloud-Ecosystem && rm -rf /tmp/accept
```

Expected: `make test` passes; `git status --porcelain` prints nothing.

- [ ] **Step 4: Acceptance run**

From `~/projects/Aleph0-Cloud-Ecosystem`: `make up && make dev`, then repeat the browser checks from Task 10 Step 13. In a second terminal:

```bash
curl -si localhost:8080/v1/me | head -5
curl -s localhost:8080/healthz; docker compose stop postgres; curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/healthz; docker compose start postgres
```

Expected: `401` with `application/problem+json`; `{"status":"ok"}` then `503`.

After logging out, reload `http://localhost:5173`: Expected: Keycloak asks for the password again.

- [ ] **Step 5: Hand off**

Ask the user before pushing the branch and opening a pull request that closes #3.
