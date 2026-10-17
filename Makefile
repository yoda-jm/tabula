# tabula: the GUI (web/, React + Vite) is built into web/dist, which the Go
# binaries embed. `make build` does both; `make dev` runs everything locally.

.PHONY: all web build test dev

all: build

web:
	cd web && npm ci && npm run build

build: web
	go build -o bin/ ./cmd/...

test:
	go test ./...
	cd web && npm test -- --run

# Emulator + tabula + Vite dev server (hot reload) on http://localhost:5173
dev:
	./scripts/dev.sh
