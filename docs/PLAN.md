# tabula — plan

tabula is a set of tools around Behringer X32 / Midas M32 consoles, written in
Go: a console emulator, and a client that connects to a console (or to the
emulator) and serves a web GUI to inspect and drive it, record shows and replay
them, and later share them with live viewers.

## Binaries

| Binary | Role |
|---|---|
| `tabula-emu` | **Pure console emulator.** Speaks the console's OSC protocol on UDP 10023 so that X32-Edit, M32-Edit, Mixing Station, Maillot's tools — and the tabula client — can connect to it. Optional read-only HTTP debug page showing the stored state, the traffic and every request it did not understand. It is not a console UI. |
| `tabula` | **Client.** Connects to a console or to `tabula-emu` over OSC and serves a web GUI on an HTTP port: connection setup, every console menu, recording, replay… |

Both share the same library (`pkg/`).

## Library

| Package | Content |
|---|---|
| `pkg/osc` | OSC 1.0 codec with the console's leniencies (missing type tags, `node` replies without a slash). |
| `pkg/schema` | The parameter tree: every address, its type, range, steps and how it is written in node/scene text. Built from Maillot's protocol document and checked against real scene files. |
| `pkg/scene` | Scene file (`.scn`) read/write. |
| `pkg/state` | In-memory state store with a change feed; used by the emulator (as the console's memory) and by the client (as a mirror of the console). |
| `pkg/emu` | The emulator's protocol server and debug HTTP page. |
| `pkg/console` *(next)* | The client side of the protocol: discovery, full sync, `/xremote` keepalive, meters, rate-limited writes. |
| `pkg/session` *(later)* | Recording and replay. |

## Milestones

### 1. Emulator — protocol core *(in progress)*
- [x] OSC codec, byte-exact against the document's examples.
- [x] Schema for every scene node (config, 32 channels, aux ins, FX returns, 16 buses, 6 matrices, main LR/M, DCAs, FX slots, outputs, 128 headamps).
- [x] Scene lines of 3 real scenes round-trip byte for byte (6351 lines).
- [ ] State store, scene load/save.
- [ ] UDP server: `/info`, `/xinfo`, `/status`, get/set of every parameter, node-address set with multiple type tags, `/node`, `/` (echoed), `/xremote` change fan-out to the *other* clients (10 s lease).
- [ ] Every request not understood is logged (stderr, JSON lines file, debug page) with client, address, type tags and arguments, so connecting X32-Edit/M32-Edit shows what is missing.
- [ ] Debug HTTP page: state tree (search), clients, traffic, unknown requests, current state as `.scn`.
- [ ] Acceptance: load GVO-2026.scn, dump it back over the network with `/node` (and Maillot's `X32GetScene`) → identical file.

### 2. Emulator — what X32-Edit / M32-Edit need
Driven by the unknown-request log while connecting the real editors:
- `/-prefs`, `/-stat`, `/-show`, `/-libs`, `/-action`, `/-usb`, `/-urec`, `/-insert`, `/-ha`.
- `/meters/0…16` with synthetic levels, `/subscribe`, `/formatsubscribe`, `/batchsubscribe`, `/renew`, `/unsubscribe`.
- Scenes/snippets/presets library: `/save`, `/load`, `/copy`, `/delete`, `/rename`, `/showdump`.
- Effect parameters (`/fx/N/par/NN`): per-effect parameter tables (61 effect types), replacing the verbatim text kept today.

### 3. Client — connection and GUI v1
- `pkg/console`: discovery (broadcast `/xinfo`), full sync via `/node` (≈3 s), `/xremote` renewal, meters, write queue.
- **Read-only by default** on a real console; writes must be armed explicitly.
- Web GUI (embedded in the binary): connection page, generic parameter browser generated from the schema (all menus early), then console-like screens (Home, Gate, Dyn, EQ, Sends, Routing, Outputs, FX, Scenes).
- WebSocket for state changes, binary frames for meters.

### 4. Recording and replay
- Session = start snapshot (`.scn`, opens in M32-Edit) + time-stamped log of every change (who/what/when) + markers.
- Replay to the emulator or a console, with seek; replaying to a real console asks for confirmation.
- Later: USB audio capture in the same process for exact sync (RF64 or one file per track: a 32-track set exceeds the 4 GB WAV limit).

### 5. Live viewers, roles, band screens
- One console connection shared by any number of browsers.
- Roles: engineer (read/write), musician (own IEM bus only, phone-friendly "my mix"), viewer (read-only).
- Board-mix audio streaming (WebRTC), per-singer IEM pages, setlist, PA calibration.

## Protocol notes

- Reference: *Unofficial X32/M32 OSC Remote Protocol*, P-G. Maillot, updated 2021-06-29 (see `docs/README.md`).
- Floats are normalized `[0, 1]` on the wire and quantized to a fixed number of steps (faders 1024, sends 161, EQ frequency 201…).
- Node text is the scene-file format. Numbers round ties away from zero (`-8.25` → `-8.3`); positive levels carry a sign (`+0.3`), zero does not (`0.0`).
- Unverified text formats, to check against a real-console `/node` dump: hold below 10 ms, release below 10 ms or above 999 ms, positive fader levels.

## Open decisions

- License: if parameter tables are ported from Maillot's GPLv3 code, tabula must be GPLv3.
