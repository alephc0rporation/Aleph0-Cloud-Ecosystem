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

.PHONY: test
test: web/node_modules
	cd api && go vet ./... && go test ./...
	cd web && npm run lint && npm run typecheck

.PHONY: generate
generate:
	cd api && go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml

.PHONY: dev-api
dev-api:
	cd api && go run ./cmd/api serve

# Reinstall when the lock file changes.
web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@

.PHONY: dev-web
dev-web: web/node_modules
	cd web && npm run dev
