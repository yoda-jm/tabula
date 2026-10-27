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
  files. Node text cannot escape a double quote: a name holding one
  (`Vox "Lead"`, set over OSC, where it is kept as sent) is written with an
  apostrophe in node text, scene files and the library (`"Vox 'Lead'"`), so
  that the line still reads back; a line a console wrote with the quote as
  it is (`"Vox "Lead""`) still reads, a quote ending a string only before a
  blank. What a real console does with it is not known.
- Get and set of every parameter, node text with `/node` and `/`, node sets
  with several values, `/xremote`: a change is pushed to the other clients.
  Changes the console makes itself (linked partners, status values) are
  pushed to every client, the one that caused them included.
- Node text sent with `/` is one line: the line feed M32-Edit leaves at its
  end is accepted, a line feed inside it or any other control character is
  refused. Effect parameters whose text does not parse are kept as text, so
  that scenes still round-trip, until one of them is set.
- Subscriptions (`/meters`, `/subscribe`, `/formatsubscribe`,
  `/batchsubscribe`) last 10 s unless renewed. `/renew` and `/unsubscribe`
  take the alias or the command, with or without its leading slash; a meter
  subscription also answers to its address alone (`/renew ,s meters/5`
  renews `/meters/5 3 1`). A client holds at most 64 subscriptions; a
  `/formatsubscribe` range covers at most 1000 numbers, every address it
  gives must exist, and a request returns at most 1024 parameters.
- Requests the emulator refuses or does not know are logged as unhandled.
  It keeps up to 1000 kinds of them (the next ones are counted together as
  `<other>`), and the first 512 bytes of each packet's text. A client is
  forgotten a minute after its last request, once it holds no subscription
  or `/xremote` lease.
- Solo: `/-stat/solo` is on while a solo switch is; with
  `/config/solo/exclusive` on, a solo switched on turns the others off;
  `/-action/clearsolo` clears them.
- Status the console computes: `/-stat/solo`, `/-insert` (where each effect
  is inserted), `/-ha/NN/index` (headamp feeding each channel, from its
  source and the input routing), `/-stat/rtasource` (from
  `/-action/setrtasrc`).
- Library: scenes, snippets and presets (`/save`, `/load`, `/copy`,
  `/delete`, `/rename`, `/add`, `/showdump`). A scene recall leaves alone
  what the safes keep, a snippet holds and changes what its filters select
  ([below](#scene-safes-and-snippet-filters)).
- A USB stick (`-usb dir`): browsing its folders, playing, winding and
  recording on its player, loading its scenes, snippets, presets and shows,
  as described [below](#usb-stick).

## Audio flow (meters)

Each meter frame is computed from the current parameters, once for all the
meter subscriptions served at the same moment:

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
   channels (fader, mute, sends, L/R). Their sends add to the buses after
   the effects took their source, so an effect does not hear its own
   return.
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

## Scene safes and snippet filters

A scene recall (`/load ,si scene N`, `/-action/goscene`, a cue) leaves
alone what is safe, a parameter at a time (a muted channel can stay muted
while its fader is recalled):

- the scene's own safes, `/-show/showfile/scene/NNN/safes` bits 1-8:
  talkback (`/config/talk`), effects (`/fx`), mix buses (`/bus`), channels
  (channels, aux inputs, FX returns, but their preamp), configuration (the
  other `/config` nodes: links, mute groups, solo, DP48, oscillator, user
  controls, USB recorder, automix), preamps (`preamp`, `delay` of the input
  channels, and every `/headamp`), output patch (`/outputs`,
  `/config/userrout`), routing (`/config/routing`). Scene 0 has none;
- the parameter safes of the show, `/-show/showfile/show`: `inputs` (bits
  0-7: preamp and delay with the headamps, config, EQ, gate and compressor,
  insert, groups and automix, fader, pan and the L/R and M/C assigns and
  level, mute) for the channels, aux inputs and FX returns; `mxsends` (their
  sends to mix 1-16); `mxbuses` (bit 0 the buses' sends to the matrices,
  then as `inputs`) for the mix buses only; `console` (configuration, solo,
  routing, output patch, user in, user out; surface state is not in a
  scene);
- the channel safes, whole strips: `chan16`, `chan32`, `return` (aux 1-8,
  FX 1L-4R), `buses`, `lrmtxdca` (matrices 1-6, L/R, M/C, DCA 1-8),
  `effects`; a safe channel keeps the headamp feeding it (`/-ha/NN/index`);
- `/-prefs/safe_masterlevels`: the L/R and M/C faders.

A snippet's filters (`/-show/showfile/snippet/NNN/eventtyp`, `channels`,
`auxbuses`, `maingrps`) say what it holds: the parameters of `eventtyp`
bits 0-12 (bit 6 fader and pan, bit 11 the L/R and M/C assigns and level,
bits 8-10 the sends to buses 1-8, 9-12, 13-16, bit 12 the sends of buses
and main to the matrices) of the selected strips (`maingrps` bits 6 and 7:
L/R and M/C, a guess following `lrmtxdca`), the effect slots of bits 13-20,
and the console's configuration, solo, routing and output patch (bits
21-24). A save keeps only that, one parameter per line as in the console's
.snp files; a recall applies what the snippet holds and its filters still
select. Headamps follow the channels they feed.

Library directories (`-library`) keep a scene's safes in its file's first
line (`#4.0# "name" "note" %000000110 1`) and a snippet's filters in its
own (`#4.0# "name" 64 3840 0 0 1`, as in a .snp file); changing them from
a client writes the file again. A scene saved again keeps the safes of its
slot, a copy takes them along, a deleted slot loses them.

The bits come from Maillot's document; which parameters each one covers is
read from its descriptions. Guesses: what the console's own and parameter
safes cover beyond those descriptions (the preamp set apart from the
channels, the mix-bus parameter safes left off the matrices and main),
`maingrps` bits 6-7, and filtering at recall as well as at save. A scene
or snippet loaded from the USB stick applies whole.

## USB stick

`-usb dir` plugs a USB stick into the emulator: the folder `dir` stands for
it (`testdata/usb` is a small one to try). The console's USB recorder is
driven as Patrick-Gilles Maillot's `X32USB` tool drives it:

```sh
tabula-emu -scene testdata/scenes/demo.scn -usb testdata/usb
```

| Parameter | What the emulator does |
|---|---|
| `/-stat/usbmounted` | 1 while the stick is in. A client setting it to 0 unmounts it (the player stops, the listing empties); 1 mounts it again. |
| `/-usb/path` | the name of the current directory, `""` at the root of the stick |
| `/-usb/dir/maxpos` | the number of entries of the current directory (up to 999) |
| `/-usb/dir/001…/name` | the entries, numbered from 1: `[..]` first in a subdirectory, then the directories as `[name]`, then the files with their extension, each group in alphabetical order (case ignored); hidden files (starting with a dot) are left out. Entries past `maxpos` are empty. |
| `/-usb/dir/dirpos` | the entry last selected, 0 after entering a directory |
| `/-usb/title` | the file selected, without its extension (`02 Opening`) |
| `/-action/recselect n` | entry `n` of the current directory: `[..]` goes up, a directory is entered, a `.wav` file plays from its start, another file is loaded (see [Loading files](#loading-files)). Then it reads 0 again. |
| `/-stat/tape/state` | 0 stop, 1 pause, 2 play, 3 recording paused, 4 record, 5 fast forward, 6 rewind (as the console's `STOP`, `PPAUSE`, `PLAY`, `RPAUSE`, `RECORD`, `FF`, `REW`). Setting 0 stops and goes back to the start of the file, 1 pauses a playing or winding file, 2 resumes where it is, or plays the stopped file again from its start. 5 and 6 wind the file in the player, playing, paused or stopped, 10 seconds for each second (a guess: the document does not say how fast the console winds), until 1, 2 or 0; wound to its end, the file ends as when played to its end; rewound to its start, the player stops there. 4 records, 3 pauses the recording and 4 resumes it; while it records, the player (winding too) and `/-action/recselect` wait. |
| `/-stat/tape/file` | the file in the player, from the root of the stick: `/Breaks/Interval music.wav`; it stays after the end or a stop. While recording, the file being recorded, named after the time in the current directory: `/R_20261026-203512.wav`; empty once the recording stops (nothing was written). |
| `/-stat/tape/etime`, `rtime` | the seconds played and left, pushed to the `/xremote` clients every second while playing or winding; the length comes from the WAV header. While recording: the seconds recorded, and `rtime` -1 (the console counts dropouts there, as −(dropouts + 1): none here). |
| `/-action/playtrack -1`, `1` | the previous or next WAV file of the player's directory, played from its start. Nothing changes at either end; with no file in the player, `1` plays the first WAV file of the current directory. |
| `/config/tape/autoplay` | `ON` (folder mode): at the end of a file the next WAV file of its directory plays, the player stops after the last; `OFF`: it stops at the end of each file |

### Loading files

The file types are those of Maillot's `X32USB` (which loads scenes,
snippets and presets with `/-action/recselect`) and of M32-Edit (`.mon`,
and the names of a show's files). The presets are read as M32-Edit writes
them (`~/.config/.M32-Edit/Xlib`), or as the emulator's `-library`
directory keeps them.

| File | What loading it does |
|---|---|
| `.scn` scene, `.snp` snippet | its lines are applied to the console, as a scene load (without scene safes) |
| `.chn` channel preset | to the strip selected on the console (`/-stat/selidx`): its configuration, preamp, gate, dynamics, EQ and sends, whatever the strip has of them |
| `.efx` effect preset | to the effect slot shown on the console's Effects screen (`/-stat/screen/FX/page` `FX1`…`FX8`), slot 1 on its home page |
| `.rou` routing preset, `.mon` AES50/DP48 preset | applied as they are (`/config/routing/…`, `/config/dp48/…`) |
| `.shw` show | replaces the show: its name and scene safes, the cues, the listings of its scenes and snippets (`showfile/…` lines as M32-Edit writes them, cues past the hundredth left out), and the scenes and snippets themselves, read from the files saved next to it as M32-Edit names them: `Gig.001.scn`, `Gig.004.snp` for `Gig.shw` (case ignored). A scene or snippet listed without its file is listed empty, and the load is logged as rejected with the first missing file. The `-library` directory is not changed: it keeps what was saved there with `/save` |
| `.prf` preferences, other files | not loaded, logged as unhandled: the format of preference files is not known |

Guesses: where the console puts a channel or an effect preset loaded from
its USB screen (the document does not say; the console may also ask), and
that a show loaded from the stick changes the show only, not the mix. A
preset none of whose lines is a parameter of where it goes is logged as
rejected.

Not emulated: no sound comes out (the WAV files are only read for their
length); a recording writes nothing to the folder;
`/-usb/dir/NNN/type` stays empty (what the console writes there is not
documented). The WAV files of `testdata/usb` are silent and sampled at
100 Hz, so a 3-minute file weighs 20 kB.

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
