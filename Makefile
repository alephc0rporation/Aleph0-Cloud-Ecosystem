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
test: web/node_modules/.package-lock.json
	cd api && go vet ./... && go test ./...
	cd web && npm run lint && npm run typecheck && npm test

.PHONY: generate
generate: web/node_modules/.package-lock.json
	cd api && go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
	cd web && npm run generate

.PHONY: dev-api
dev-api:
	cd api && go run ./cmd/api serve

# Reinstall when the lock file changes. npm writes this marker only after a successful
# install, so a failed or interrupted one runs again.
web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci

.PHONY: dev-web
dev-web: web/node_modules/.package-lock.json
	cd web && npm run dev

.PHONY: dev
# Ctrl+C stops both.
dev: web/node_modules/.package-lock.json
	$(MAKE) -j2 dev-api dev-web

# Deploy toolchain (WSL only). The marker is written last, so a failed install runs again.
deploy/.venv/.done: deploy/requirements.txt deploy/requirements.yml
	python3 -m venv deploy/.venv
	deploy/.venv/bin/pip install --upgrade pip
	deploy/.venv/bin/pip install -r deploy/requirements.txt
	PATH="$(CURDIR)/deploy/.venv/bin:$$PATH" deploy/.venv/bin/kolla-ansible install-deps
	deploy/.venv/bin/ansible-galaxy collection install -r deploy/requirements.yml
	touch $@

# Infrastructure (WSL only). ENV picks deploy/nodes/$(ENV).yml.
ENV ?= dev

.PHONY: infra-test
infra-test: deploy/.venv/.done
	deploy/infra.sh test $(ENV)

.PHONY: infra-apply infra-destroy
infra-apply: deploy/.venv/.done
	deploy/infra.sh apply $(ENV)

infra-destroy:
	deploy/infra.sh destroy $(ENV)

.PHONY: node-remove
node-remove: deploy/.venv/.done
	deploy/infra.sh remove $(ENV) $(NODE)

# Free the dev VMs' RAM, and bring them back.
.PHONY: infra-stop infra-start
infra-stop: deploy/.venv/.done
	deploy/infra.sh stop $(ENV)

infra-start: deploy/.venv/.done
	deploy/infra.sh start $(ENV)

# Apply globals.yml or Ceph key changes to a running cluster. TAGS=glance,cinder limits it.
.PHONY: infra-reconfigure
infra-reconfigure: deploy/.venv/.done
	deploy/infra.sh reconfigure $(ENV) $(TAGS)
