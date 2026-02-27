BINARY := pgmigrator
PKG := ./cmd/pgmigrator

.PHONY: build test lint clean web-install web-build web-dev build-full docker install

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -v

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
