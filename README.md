# tabula

[![CI](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml/badge.svg)](https://github.com/yoda-jm/tabula/actions/workflows/ci.yml)
· [Site](https://yoda-jm.github.io/tabula/) · [Releases](https://github.com/yoda-jm/tabula/releases)

Tools for Behringer X32 / Midas M32 digital consoles, in Go.

- **`tabula-emu`** — a console emulator speaking the console's OSC protocol
  (UDP 10023), to develop and test without a console. X32-Edit, M32-Edit and
  other remote apps can connect to it. A read-only HTTP page shows what it
  stores and every request it did not understand.
- **`tabula`** — a client that connects to a console or to the emulator and
  serves a web GUI: mixer, channel details with headamps, routing, effects
  with a graphic EQ and RTA, scenes and presets, every parameter, recording
  and replay of shows (with the audio tracks of the console's USB card, see
  [recording](docs/recording.md)). Read-only until you arm writes.

See [how the emulator simulates the console](docs/emulator.md) (audio flow,
meters, RTA, effects, linked strips), [the pages of the GUI](docs/gui.md) and
[docs/PLAN.md](docs/PLAN.md) for the roadmap.

## Status

Early development. The parameter schema covers every node of a scene file and
round-trips real scenes byte for byte.

## Download

Latest build of `main` (both programs, GUI included):
[Linux x86-64](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-linux-amd64.tar.gz) ·
[Linux ARM64](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-linux-arm64.tar.gz) ·
[Linux ARM](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-linux-arm.tar.gz) ·
[macOS Apple silicon](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-darwin-arm64.tar.gz) ·
[macOS Intel](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-darwin-amd64.tar.gz) ·
[Windows](https://github.com/yoda-jm/tabula/releases/download/latest/tabula-windows-amd64.zip).
Versioned releases: [releases page](https://github.com/yoda-jm/tabula/releases).

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
./bin/tabula -console 127.0.0.1 # or the console's IP; -http :8033 to reach it from a phone
```

and open <http://localhost:8033>. tabula starts **read-only**: arm writes
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
