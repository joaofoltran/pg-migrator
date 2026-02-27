BINARY := pgmigrator
PKG := ./cmd/pgmigrator

.PHONY: build test test-integration lint clean web-install web-build web-dev build-full docker install

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -v

CONTAINER_RT := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)
COMPOSE_CMD := $(shell \
	if command -v docker >/dev/null 2>&1; then echo "docker compose"; \
	elif command -v podman-compose >/dev/null 2>&1; then echo "podman-compose"; \
	elif command -v podman >/dev/null 2>&1; then echo "podman compose"; \
	fi)

test-integration:
ifndef CONTAINER_RT
	$(error No container runtime found. Install docker or podman.)
endif
	$(COMPOSE_CMD) -f docker-compose.test.yml up -d --wait
	go test -tags=integration -v -count=1 -timeout=300s ./internal/pipeline/ || ($(COMPOSE_CMD) -f docker-compose.test.yml down -v && exit 1)
	$(COMPOSE_CMD) -f docker-compose.test.yml down -v

lint:
	go vet ./...

clean:
	rm -f $(BINARY)

# Frontend targets
web-install:
	cd web && npm ci

web-build: web-install
	cd web && npm run build
	rm -rf internal/server/dist
	mkdir -p internal/server/dist
	cp -r web/dist/* internal/server/dist/

web-dev:
	cd web && npm run dev

# Full build: frontend + Go binary
build-full: web-build build

# Docker
docker:
	docker build -t pgmigrator .

# Install to /usr/local/bin
install: build-full
	cp $(BINARY) /usr/local/bin/
