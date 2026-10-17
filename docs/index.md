---
title: tabula
---

# tabula

Tools for **Behringer X32 / Midas M32** digital consoles, written in Go.

## tabula-emu — a console on your laptop

`tabula-emu` speaks the console's OSC protocol on UDP port 10023, so remote
apps — X32-Edit, M32-Edit, Mixing Station, the tabula client — can connect to
it as if it were a console. Use it to prepare a show offline, to develop and
test software, or to learn the console without one.

```sh
tabula-emu -scene my-show.scn -save my-show.scn -http :8032
```

- `-scene` loads a scene file at start, `-save` writes the state back on exit.
- `-http` opens a **read-only debug page**: every parameter (searchable), the
  connected clients, the traffic, and every request the emulator did not
  understand — connect an editor and see what is missing.
- `-unhandled-log file.jsonl` appends every unhandled request to a file.
- `-model` picks what `/info` reports: X32, X32RACK, X32C, X32P, X32CORE, M32, M32R, M32C.

Downloads are on the [releases page](https://github.com/yoda-jm/tabula/releases).

### What works today

- Every parameter of a scene file: configuration and routing, 32 channels,
  8 aux ins, 8 FX returns, 16 buses, 6 matrices, main LR and mono, 8 DCAs,
  FX slots, outputs, 128 headamps.
- `/info`, `/xinfo`, `/status`, get and set of every parameter (enums by
  name or index, floats quantized to the console's known values), node sets
  with multiple type tags, `/node`, `/` (echoed), and `/xremote`: changes
  are pushed to the other connected clients.
- Real scenes round-trip byte for byte, also when dumped over the network
  with Patrick-Gilles Maillot's `X32GetScene`.
- Preferences and status (`/-prefs`, `/-stat`, `/-show`, `/-libs`, `/-usb`,
  `/-urec`, `/-action`…), as the editors query them when connecting.
- Meters (`/meters/0…16`) with synthetic levels that follow the mix, and
  `/subscribe`, `/formatsubscribe`, `/batchsubscribe`, `/renew`, `/unsubscribe`.

### Not yet

Most effect parameters (only HALL is modelled; other effects are kept as
text), the scene/preset library commands, and the side effects of actions:
see the [plan](PLAN.md).

## tabula — the client *(coming)*

A program that connects to a console or to the emulator and serves a web GUI:
connection setup, every console menu, recording and replay of shows, and
later live viewers and band-specific screens (in-ear mixes from a phone…).

## Reference

The protocol work relies on Patrick-Gilles Maillot's *Unofficial X32/M32 OSC
Remote Protocol* and his tools: <https://sites.google.com/site/patrickmaillot/x32>.

Source code: <https://github.com/yoda-jm/tabula>
