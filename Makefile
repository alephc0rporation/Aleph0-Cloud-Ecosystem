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
test:
	cd api && go vet ./... && go test ./...
