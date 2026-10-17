---
title: The tabula GUI
---

# The tabula GUI

`tabula` serves its GUI on its HTTP port (<http://localhost:8033> by
default; start it with `-http :8033` to reach it from a phone or a tablet).
Every page works on a phone as well as on a laptop. Any number of browsers
can be open at once: they share the one console connection, and a change
made in one shows in the others.

**Read-only until armed.** Controls are greyed out until you arm writes
(top right), after a confirmation. On a real console nothing moves by
accident. Disarm to go back to read-only.

## Mixer

Banks of strips, as on the console: inputs 1-16 and 17-32, aux inputs and
FX returns, buses, matrices and mains, DCAs. Each strip has its scribble
strip (name and colour), meter, fader, mute and pan. Tap a scribble strip to
select it for the Channel page.

## Channel

Everything about one strip: configuration, preamp, delay, gate, dynamics,
EQ with its response curve, insert, main mix, sends with the bus names,
DCA and mute groups, automix. Only the sections the strip has are shown.

**Headamp.** For a channel (or an aux input) fed by a preamp, the Preamp
section starts with that preamp: which input it is (`AES50 A 12`: input 12
of the stage box on AES50 A), its gain and its +48 V phantom power. The
console tells which headamp feeds each channel (`/-ha/NN/index`), following
the channel's source and the input routing; the gain and phantom are
`/headamp/NNN/gain` and `phantom`. Switching phantom power asks for a
confirmation, as it pops through the PA. When the input has no preamp
(card, user input, a bus…), the section says where it comes from instead.
The Source row of the Config section also shows the physical input
(`In 12 · AES50 A 12`).

## Browser

Every parameter of the console, grouped by section and searchable by path
or by value (`/ch/01/eq`, `Remy`, `GATE`). Each node opens into an editor of
its fields; a value can also be typed as on the console (`-6`, `1k20`,
`GATE`).

## Record

Record a show: a snapshot of the console when you start (a scene file
that opens in M32-Edit) plus every change with its time, and markers you add
(press M). Replay a session onto the console or the emulator, from a time or
a marker, at a chosen speed (writes must be armed). See the
[API](API.md#recording-and-replay) for the session format.

## Connect

Find the consoles on the network (a broadcast that X32/M32 consoles and
`tabula-emu` answer) or type an address, connect and follow the sync.
