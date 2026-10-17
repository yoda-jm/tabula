# tabula: the GUI (web/, React + Vite) is built into web/dist, which the Go
# binaries embed. `make build` does both; `make dev` runs everything locally.

.PHONY: all web build test dev dist

all: build

web:
	cd web && npm ci && npm run build

build: web
	go build -o bin/ ./cmd/...

# Release archives for every platform in dist/ (VERSION=v1.2.3 make dist).
dist: web
	./scripts/dist.sh $${VERSION:-dev} $${VERSION:-dev}

test:
	go test ./...
	cd web && npm test -- --run

# Emulator + tabula + Vite dev server (hot reload) on http://localhost:5173
dev:
	./scripts/dev.sh
