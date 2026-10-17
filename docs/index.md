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
- `-library dir` keeps the scenes, snippets and presets saved with `/save`
  (from an editor's library pages) in a directory, one file each.
- `-model` picks what `/info` reports: X32, X32RACK, X32C, X32P, X32CORE, M32, M32R, M32C.

Downloads are on the [releases page](https://github.com/yoda-jm/tabula/releases).

### What works today

- Every parameter of a scene file: configuration and routing, 32 channels,
  8 aux ins, 8 FX returns, 16 buses, 6 matrices, main LR and mono, 8 DCAs,
  FX slots with the parameters of all 61 effects, outputs, 128 headamps.
- `/info`, `/xinfo`, `/status`, get and set of every parameter (enums by
  name or index, floats quantized to the console's known values), node sets
  with multiple type tags, `/node`, `/` (echoed), and `/xremote`: changes
  are pushed to the other connected clients.
- Real scenes round-trip byte for byte, also when dumped over the network
  with Patrick-Gilles Maillot's `X32GetScene`.
- The scene, snippet and preset library: `/save`, `/load`, `/copy`,
  `/delete`, `/rename`, `/add`, `/showdump`.
- Preferences and status (`/-prefs`, `/-stat`, `/-show`, `/-libs`, `/-usb`,
  `/-urec`, `/-action`…), as the editors query them when connecting.
- Meters (`/meters/0…16`) with synthetic levels that follow the mix, and
  `/subscribe`, `/formatsubscribe`, `/batchsubscribe`, `/renew`, `/unsubscribe`.

### Not yet

Scene safes and snippet filters on recall: see the [plan](PLAN.md).

## tabula — the client

`tabula` connects to a console (or to `tabula-emu`) and serves a web GUI,
from the same binary, to any browser on the network — a laptop at the desk
or a phone on stage.

```sh
tabula -console 192.168.1.62 -http :8080
```

- **Mixer**: banks of strips (inputs, aux and FX returns, buses, matrices
  and mains, DCAs) with scribble strips in the console colours, meters,
  faders, mutes and pans.
- **Channel**: everything about one strip — config, preamp, gate, dynamics,
  EQ with its (approximate) response curve, sends with the bus names.
- **Browser**: every parameter of the console, searchable by path or value,
  each with an editor; values can also be typed as on the console (`-6`,
  `1k20`, `GATE`).
- **Record**: record a show (snapshot plus every change, with markers) and
  replay it onto the console or the emulator.
- **Connect**: find consoles on the network or type an address.

It is **read-only** until writes are armed, after a confirmation: on a real
console, nothing moves by accident. `-record` starts recording as soon as
the console is synced, for unattended recording of a concert.

Later: live viewers and band-specific screens (in-ear mixes from a phone…).

## Reference

The protocol work relies on Patrick-Gilles Maillot's *Unofficial X32/M32 OSC
Remote Protocol* and his tools: <https://sites.google.com/site/patrickmaillot/x32>.

Source code: <https://github.com/yoda-jm/tabula>
