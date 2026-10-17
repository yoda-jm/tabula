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
| `pkg/console` | The client side of the protocol: discovery, full sync, `/xremote` keepalive, meters, rate-limited writes. |
| `pkg/session` *(later)* | Recording and replay. |

## Milestones

### 1. Emulator — protocol core *(done)*
- [x] OSC codec, byte-exact against the document's examples.
- [x] Schema for every scene node (config, 32 channels, aux ins, FX returns, 16 buses, 6 matrices, main LR/M, DCAs, FX slots, outputs, 128 headamps).
- [x] Scene lines of 3 real scenes round-trip byte for byte (6351 lines).
- [x] State store, scene load/save.
- [x] UDP server: `/info`, `/xinfo`, `/status`, get/set of every parameter, node-address set with multiple type tags, `/node`, `/` (echoed), `/xremote` change fan-out to the *other* clients (10 s lease).
- [x] Every request not understood is logged (stderr, JSON lines file, debug page) with client, address, type tags and arguments, so connecting X32-Edit/M32-Edit shows what is missing.
- [x] Debug HTTP page: state tree (search), clients, subscriptions, traffic, unhandled requests, current state as `.scn`.
- [x] Acceptance: GVO-2026.scn dumped back over the network with `/node` and with Maillot's `X32GetScene` → identical node lines.

### 2. Emulator — what X32-Edit / M32-Edit need *(in progress)*
Driven by the unknown-request log while connecting the real editors:
- [x] `/-prefs`, `/-stat`, `/-show`, `/-libs`, `/-action`, `/-undo`, `/-usb`, `/-urec`, `/-insert`, `/-ha` (1126 nodes; formats checked against a real M32C's values cached by M32-Edit).
- [x] `/meters/0…16` with synthetic levels following the mix, `/subscribe`, `/formatsubscribe`, `/batchsubscribe`, `/renew`, `/unsubscribe`.
- [ ] Effect parameters (`/fx/N/par/NN`): the framework is there (fields depend on the effect type; switching effect loads its defaults); HALL is modelled, the other effects are kept as text until their tables land.
- [x] Scenes/snippets/presets library: `/save`, `/load`, `/copy`, `/delete`, `/rename`, `/add` (cues), `/showdump`, with listings in `/-show` and `/-libs`, optionally kept in a directory (`-library`). Scene safes and snippet filters are not applied yet.
- [x] Side effects of `/-action/*` (go scene/snippet/cue, clear solo, undo checkpoint and undo, init) and computed status (`/-insert`, `/-ha/NN/index` from sources and input routing, `/-stat/solo`).
- [ ] Connect X32-Edit and M32-Edit, work through the unhandled-request log.

### 3. Client — connection and GUI v1 *(in progress)*
- [x] `pkg/console`: discovery (broadcast `/xinfo`), full sync via `/node` (window of 32 requests), `/xremote` renewal, writes refused until armed; tested end to end against the emulator.
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
