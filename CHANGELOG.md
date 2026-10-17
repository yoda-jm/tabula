# Changelog

## Unreleased

### tabula-emu
- Accepts node text ending with a line feed, as M32-Edit sends it when
  writing a scene to the console ("PC -> Mixer").
- The RTA (`/meters/15`) analyses the source chosen with `/-action/setrtasrc`
  (now mirrored in `/-stat/rtasource`): a channel, bus, matrix or main before
  or after its EQ, or the input of a graphic EQ. The low cut, the EQ and the
  graphic EQ bands shape it, so the RTA behind M32-Edit's EQ and GEQ pages
  follows the edits. It used to show the mains only, too low to be drawn.
- Linked strips are kept in step as on the console: a change on one strip of
  a linked pair (channels, aux inputs, FX returns, buses, matrices) is
  applied to the other and pushed to every client; linking a pair copies
  the odd strip to the even one; `/config/linkcfg` chooses what follows
  (headamp and delay, EQ, dynamics, fader and mute). Name, icon, source,
  pans, key sources and insert selection never follow. M32-Edit relies on
  the console for this when connected.
- New page: [how the emulator simulates the console](docs/emulator.md).
- `/-prefs/rta` fields 11-14 have their real names (`rtaoneq`, `rtaongeq`,
  `range`, `gain30`) and RTA sources go up to 195 (98 sources, before or
  after the EQ).

### Downloads
- A `latest` release is rebuilt from `main` after every change that passes
  CI, with archive names that never change
  (`releases/download/latest/tabula-linux-amd64.tar.gz`…), linked from the
  site's download section. `make dist` builds the same archives locally.

## v0.1.0 — first pre-release

### tabula-emu, the console emulator
- Speaks the X32/M32 OSC protocol on UDP 10023 (IPv4, so editors find it by
  broadcast): `/info`, `/xinfo`, `/status`, get/set of every parameter,
  node sets with multiple type tags, `/node`, `/` (echoed), `/xremote`.
- Every parameter of a scene file, plus preferences and status (`/-prefs`,
  `/-stat`, `/-show`, `/-libs`, `/-usb`, `/-urec`, `/-action`, `/-undo`,
  `/-insert`, `/-ha`): about 3240 nodes and 12800 parameters. Real scenes
  round-trip byte for byte, also through Maillot's `X32GetScene`.
- Effect parameters follow the effect loaded in each slot; all 61 effects are
  modelled (ranges, formats and defaults as in M32-Edit).
- Meters (`/meters/0…16`) with synthetic levels that follow the mix, and
  `/subscribe`, `/formatsubscribe`, `/batchsubscribe`, `/renew`, `/unsubscribe`.
- Scene, snippet and preset library (`/save`, `/load`, `/copy`, `/delete`,
  `/rename`, `/add`, `/showdump`), optionally kept in a directory; M32-Edit's
  factory presets can be imported.
- Actions (go scene/snippet/cue, clear solo, undo) and computed status.
- Read-only debug page: state, clients, subscriptions, traffic, and every
  request not handled (also as a JSON lines file).

### tabula, the client
- Connects to a console or the emulator, mirrors its full state, follows
  changes, survives a console reboot; writes refused until armed.
- Web GUI embedded in the binary (React): Mixer with meters, Channel (with
  an EQ curve), Browser for every parameter, Record (sessions, markers,
  replay), Connect; usable on a laptop or a phone.
- HTTP API with a live event stream (changes, status, meters), see
  `docs/API.md`.
- Records shows (snapshot that opens in M32-Edit + every change with its
  time + markers) and replays them with seek and speed; `-record` for
  unattended recording.
