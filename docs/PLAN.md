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
| `pkg/session` | Recording and replay; exports for audio editors. |
| `pkg/audio` | Audio capture of the console's USB card: one WAV file per track, sync with the session. |

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
- [x] Effect parameters (`/fx/N/par/NN`): fields follow the effect type (switching effect loads its defaults); all 61 effects modelled, with ranges, steps, formats and defaults matching M32-Edit's built-in tables; 57 of 61 real parameter lines round-trip byte for byte, the other 4 (older firmware spellings) load and are written in today's form.
- [x] Scenes/snippets/presets library: `/save`, `/load`, `/copy`, `/delete`, `/rename`, `/add` (cues), `/showdump`, with listings in `/-show` and `/-libs`, optionally kept in a directory (`-library`). Scene safes and snippet filters are not applied yet.
- [x] Side effects of `/-action/*` (go scene/snippet/cue, clear solo, undo checkpoint and undo, init) and computed status (`/-insert`, `/-ha/NN/index` from sources and input routing, `/-stat/solo`).
- [x] Factory presets: `-presets` imports M32-Edit's library (channel, effect and routing presets).
- [x] RTA of the source chosen with `/-action/setrtasrc` (channels, buses, graphic EQs, pre/post EQ), shaped by low cut, EQ and graphic EQ.
- [x] Linked strips (channels, aux, FX returns, buses, matrices) kept in step, following `/config/linkcfg`, with the rules of M32-Edit's offline logic (see `docs/emulator.md`).
- [ ] Connect X32-Edit and M32-Edit, work through the unhandled-request log (guide: `docs/testing-with-editors.md`).

### 3. Client — connection and GUI v1 *(in progress)*
- [x] `pkg/console`: discovery (broadcast `/xinfo`), full sync via `/node` (window of 32 requests), `/xremote` renewal, writes refused until armed; tested end to end against the emulator.
- **Read-only by default** on a real console; writes must be armed explicitly.
- [x] Web GUI (React + TypeScript, embedded in the binary): Connect, Mixer (banks, meters, faders, mutes, pans), Channel (config, preamp, gate, dynamics, EQ curve, sends), Browser (every parameter with an editor), Record.
- [x] Server-Sent Events for state changes, status and meters.
- [ ] Console-like screens still to come: Routing, Outputs, FX pages, Scenes/library, headamp gain/phantom on the Channel page.

### 4. Recording and replay *(in progress)*
- [x] Session = start snapshot (`.scn`, opens in M32-Edit) + time-stamped log of every change (who/what/when) + markers (`pkg/session`).
- [x] Replay to the emulator or a console, with seek and speed; refused unless writes are armed. API in `docs/API.md`; `tabula -record` records unattended.
- [x] GUI for recording, markers, session browsing and replay.
- [x] Audio of the console's USB card recorded with the session, in the same process (`pkg/audio`): an external recorder (arecord, parecord, ffmpeg or a custom command; no cgo) streams the card, one WAV file per track (24-bit by default, RF64 past 4 GiB, headers refreshed every 2 s), named from the console's routing; first-sample time and clock drift measured from the arrival of the samples and kept in `meta.json`; free space watched, clean stop before the disk is full; device listing; `-audio-*` flags (`docs/recording.md`).
- [x] Exports for audio editors: Reaper project, Audacity label track, CSV of changes and markers.
- [ ] Try the audio capture with the real DN32-USB card (Linux, then macOS/Windows through ffmpeg).

### 5. Live viewers, roles, band screens *(in progress)*
- [x] One console connection shared by any number of browsers.
- [x] Roles checked by the server: engineer (this machine, or an engineer link), musician (own in-ear bus only, phone page "In-ears", works without arming when allowed), viewer (read-only); access links with QR codes on the Access page, kept in `access.json`, revocable at once (`docs/access.md`).
- [ ] Board-mix audio streaming (WebRTC), setlist, PA calibration.

## Protocol notes

- Reference: *Unofficial X32/M32 OSC Remote Protocol*, P-G. Maillot, updated 2021-06-29 (see `docs/README.md`).
- Floats are normalized `[0, 1]` on the wire and quantized to a fixed number of steps (faders 1024, sends 161, EQ frequency 201…).
- Node text is the scene-file format. Numbers round ties away from zero (`-8.25` → `-8.3`); positive levels carry a sign (`+0.3`), zero does not (`0.0`).
- Unverified text formats, to check against a real-console `/node` dump: hold below 10 ms, release below 10 ms or above 999 ms, positive fader levels.

## Open decisions

- License: if parameter tables are ported from Maillot's GPLv3 code, tabula must be GPLv3.
