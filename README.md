# tabula

[![CI](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml/badge.svg)](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml)
· [Site](https://yoda-jm.github.io/tabula/) · [Releases](https://github.com/yoda-jm/tabula/releases)

Tools for Behringer X32 / Midas M32 digital consoles, in Go.

- **`tabula-emu`** — a console emulator speaking the console's OSC protocol
  (UDP 10023), to develop and test without a console. X32-Edit, M32-Edit and
  other remote apps can connect to it. A read-only HTTP page shows what it
  stores and every request it did not understand.
- **`tabula`** — a client that connects to a console or to the emulator and
  serves a web GUI: mixer, channel details, every parameter, recording and
  replay of shows. Read-only until you arm writes.

See [docs/PLAN.md](docs/PLAN.md) for the roadmap.

## Status

Early development. The parameter schema covers every node of a scene file and
round-trips real scenes byte for byte.

## Quick start

```sh
go run ./cmd/tabula-emu -scene testdata/scenes/GVO-2026.scn -http :8032
```

Then point X32-Edit / M32-Edit at this machine's IP and open
<http://localhost:8032> to see the state, the traffic and every request the
emulator did not handle.

To drive it (or a real console) from a browser:

```sh
make build                      # GUI (needs Node 22) + binaries in bin/
./bin/tabula -console 127.0.0.1 # or the console's IP; -http :8080 to reach it from a phone
```

and open <http://localhost:8080>. tabula starts **read-only**: arm writes
(top right) to move faders on the console.

## Build

```sh
make build   # cd web && npm ci && npm run build, then go build -o bin/ ./cmd/...
make test    # go test ./... and the GUI unit tests
make dev     # emulator + tabula + Vite dev server with hot reload (http://localhost:5173)
```

The GUI lives in `web/` (React, TypeScript, Vite) and is embedded in the
`tabula` binary; a plain `go build` without it serves a page saying so.

## Name

*Tabula* is Latin for board or table — a mixing desk is a *table de mixage*.
