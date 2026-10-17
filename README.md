# tabula

[![CI](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml/badge.svg)](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml)
· [Site](https://yoda-jm.github.io/tabula/) · [Releases](https://github.com/yoda-jm/tabula/releases)

Tools for Behringer X32 / Midas M32 digital consoles, in Go.

- **`tabula-emu`** — a console emulator speaking the console's OSC protocol
  (UDP 10023), to develop and test without a console. X32-Edit, M32-Edit and
  other remote apps can connect to it. A read-only HTTP page shows what it
  stores and every request it did not understand.
- **`tabula`** *(coming)* — a client that connects to a console or to the
  emulator and serves a web GUI: connection setup, all console menus,
  recording and replay of shows.

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

## Build

```sh
go build ./...
go test ./...
```

## Name

*Tabula* is Latin for board or table — a mixing desk is a *table de mixage*.
