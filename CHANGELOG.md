# Changelog

## Unreleased

### tabula
- Access for the band: the engineer makes access links (with QR codes) on a
  new Access page. A musician's link opens their in-ear mix only, on a page
  made for phones: their bus volume and mute, a send slider per source,
  pins, stereo pan for linked buses. Viewer links see everything read-only.
  The machine running tabula stays the engineer; other devices without a
  link see nothing (`-guest` changes that). The server checks every write,
  musicians change their mix without arming, and deleting a link ends it
  at once. See [access](docs/access.md).
- Changes are recorded with who made them through a link (`web Remy`).
- Channel page: the headamp feeding a channel or aux input — which input it
  is (`AES50 A 12`), its gain and its phantom power (switched after a
  confirmation) — and the physical input next to the channel source.
- New Routing page: the input routing and patch (each input with its headamp
  and the channels using it), the outputs 1-16 with their sources and where
  each one goes, and channel by channel what AES50 A and B (a DL32's outputs),
  the card, P16, aux, AES/EBU and the local outputs carry, with the names of
  the strips. Edited with the console's routing blocks.
- New FX page: the 8 effect slots with the effect loaded in each and where
  it is used (the bus feeding FX 1-4 and their returns, the strips each slot
  is inserted on), and an editor per slot generated from the effect's
  parameters; loading another effect asks for a confirmation. Graphic EQs
  (GEQ, GEQ2, TEQ, TEQ2) get 31 band faders and a master, side A or B, with
  the RTA (`/meters/15`) behind the bands; the RTA source is pointed at the
  EQ only when writes are armed.
- The Channel page draws the RTA behind the EQ curve when the RTA listens to
  that strip, and can point it there (before or after the EQ).
- The GUI asks for extra meter groups (the RTA) only while a page shows
  them, reopening its event stream without a gap.
- New page describing [every page of the GUI](docs/gui.md).

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
