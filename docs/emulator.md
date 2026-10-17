---
title: How tabula-emu works
---

# How tabula-emu simulates the console

`tabula-emu` is a **console without audio**: it holds every parameter of an
X32/M32, speaks its OSC protocol, and computes what a console computes on its
own (meters, status, links between strips). No sound goes in or out; levels
come from a synthetic signal that follows what you do on the mix, so meters
and the RTA react like on a console with a band playing.

This page says what is simulated, how, and what is not.

## Parameters and protocol

- Every parameter of a scene (about 3240 nodes, 12800 parameters), plus the
  preferences and status pages (`/-prefs`, `/-stat`, `/-show`, `/-libs`,
  `/-usb`, `/-urec`, `/-action`…). Values are stored as the console stores
  them: floats quantized to its steps, text written exactly as in its scene
  files.
- Get and set of every parameter, node text with `/node` and `/`, node sets
  with several values, `/xremote`: a change is pushed to the other clients.
  Changes the console makes itself (linked partners, status values) are
  pushed to every client, the one that caused them included.
- Status the console computes: `/-stat/solo`, `/-insert` (where each effect
  is inserted), `/-ha/NN/index` (headamp feeding each channel, from its
  source and the input routing), `/-stat/rtasource` (from
  `/-action/setrtasrc`).
- Library: scenes, snippets and presets (`/save`, `/load`, `/copy`,
  `/delete`, `/rename`, `/add`, `/showdump`). A recall applies the stored
  values as they are; scene safes and snippet filters are not applied yet.

## Audio flow (meters)

Each meter frame is computed from the current parameters:

1. **Inputs.** Every channel with an input source gets a signal breathing
   between about −42 and −14 dBFS, different for each channel. Aux inputs
   carry no signal. Headamp gain and trim do not change it.
2. **Channel strip.** The gate attenuates by its range while the signal is
   under the threshold; the compressor applies its threshold and ratio (no
   knee, attack or release). Then mute and fader.
3. **Sends.** Each bus send applies its on switch and level, taken after the
   fader when its tap is POST, before it otherwise.
4. **Buses.** Sends add up in power; then bus dynamics, mute and fader. A bus
   assigned to the main L/R feeds it.
5. **FX returns.** FX slots 1-4 fed from a bus (`/fx/N/source`) return half
   of that bus's level into FX returns 1-8; their strips then work like
   channels (fader, mute, sends, L/R).
6. **Mains.** Channels and buses assigned to L/R add up there, and to the
   mono bus through their mono level; then dynamics, mute and fader.
7. **Matrices.** Fed by their sends from L/R, mono and the buses; then
   dynamics, mute and fader.
8. **DCAs** show the loudest of their channels. **Outputs** show the level
   of the source patched to them.

Not simulated: pan and balance (left and right always carry the same level),
delay, polarity, the EQ (it only shapes the RTA, below), insert effects,
mute groups and DCA faders/mutes acting on the sound, solo and the monitor
bus, talkback, oscillator, automix, USB/card playback.

The meter blobs have the sizes the editors expect (M32-Edit drops a blob of
the wrong size): `/meters/0` 70 values, 1: 96, 2: 49, 3: 22, 4: 82, 5: 27,
6: 4, 7: 16, 8: 6, 9: 32, 10: 32, 11: 5, 12: 4, 13: 48, 14: 80, 15: 50
(100 packed RTA bands), 16: 48.

## RTA

`/meters/15` analyses one source, chosen by the editor with
`/-action/setrtasrc` (the emulator keeps it in `/-stat/rtasource`):

| Source | Meaning |
|---|---|
| 0-31 | channels 1-32 |
| 32-39 | aux inputs 1-8 |
| 40-47 | FX returns 1L-4R |
| 48-63 | buses 1-16 |
| 64-69 | matrices 1-6 |
| 70, 71 | main L/R, mono |
| 72, 73 | monitor, talkback |
| 74-89 | dual graphic EQ A/B of FX slots 1-8 |
| 90-97 | stereo graphic EQ of FX slots 1-8 |
| + 98 | the same, after the EQ |

M32-Edit selects the channel shown on its EQ page, or the graphic EQ open on
its FX page. A graphic EQ hears the strip it is inserted on.

The spectrum is synthetic: a broad hump around the source's level, moving a
little. The channel low cut shapes it, and after the EQ (source + 98) so do
the EQ bands or the graphic EQ faders: move a band and the RTA follows.
A source with no signal shows nothing, as on a console: with every fader
and send down (a fresh scene), the buses, matrices and their graphic EQs
are silent.

M32-Edit draws the RTA only above −(range + gain) dB, from the `/-prefs/rta`
values: 30 dB range and the RTA gain, or 60 dB range and `gain30` when
`range` is ON.

## Effects

The emulator stores effect parameters but does not process sound.
`/fx/N/par/01…64` follow the effect loaded in the slot: all 61 effects are
modelled with the ranges, steps, formats and defaults of M32-Edit, and
loading another effect resets the slot to its defaults. The FX send and
return meters (`/meters/9`) follow the FX returns; the effects' own meters
(`/meters/10` and `14`) stay idle.

## Linked strips

Odd and even strips can be linked in pairs: channels (`/config/chlink/1-2`
… `31-32`), aux inputs (`auxlink`), FX returns (`fxlink`), buses
(`buslink`) and matrices (`mtxlink`). The **console** keeps a linked pair in
step; editors rely on it when connected (M32-Edit only links strips itself
in its offline mode). The emulator does the same:

- A change a client makes on one strip of a linked pair is applied to the
  other strip, and pushed to every client.
- Linking a pair copies the odd strip to the even one.
- Turning a link option on copies, in every linked pair, what that option
  covers from the odd strip to the even one.

What follows the partner:

| Parameters | Linked |
|---|---|
| name, icon, input source | never |
| pan, send pans | never |
| gate and compressor key sources, insert selection (FX5L / FX5R…) | never |
| delay, preamp (trim, polarity, low cut); for channels, the headamp of their inputs (gain, phantom) | with `/config/linkcfg/hadly` |
| EQ | with `/config/linkcfg/eq` |
| gate, compressor, insert on and position | with `/config/linkcfg/dyn` |
| fader, mute, L/R and mono assignments, mono level | with `/config/linkcfg/fdrmute` |
| everything else: colour, sends (on, level, tap), DCA and mute groups, automix | always |

These rules come from M32-Edit's offline logic, and match the linked pairs
saved by a real M32C: in its scenes the two strips of a pair differ only in
name, icon, source, pans and insert selection.

Recalling a scene or a snippet applies the stored values without linking
them.

## Verified with M32-Edit

Mixer → PC (M32-Edit reading the emulator) runs without any unhandled
request. Checked in M32-Edit: meters, the RTA on the EQ page and in the
graphic EQ editor (pre and post, following the faders), and linked channels
moving together. Fixed along the way:

- PC → Mixer: M32-Edit writes each node with the line feed of `/node`
  replies, which the emulator rejected; it now accepts it (tested by
  writing a whole scene the way M32-Edit does).
- RTA: source numbering as above, and the four undocumented `/-prefs/rta`
  fields named as M32-Edit names them (`rtaoneq`, `rtaongeq`, `range`,
  `gain30`).
- Linked strips, as above.

See [testing with the editors](testing-with-editors.md) to check it
yourself.
