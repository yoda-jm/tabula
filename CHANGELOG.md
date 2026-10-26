# Changelog

## Unreleased

### tabula
- The GUI opens on the mixer when tabula is already connected to a console
  (it showed the Connect page until the first status came).
- Scribble strips show names of two words on two lines (`Guitare Elec`,
  `Vx Bastien`) instead of cutting them, on phones too.
- Channel page: the EQ comes right after the preamp, so its curve shows
  when the page opens, and the sections fill balanced columns without
  holes; the fields of the Main section use the card's whole width.
- Record page: click the audio tracks to record in a list that names them
  after what the console sends to its card (**All**, **Named**); the clicks
  write *Tracks to record* (`1-16,23-24`). `GET /api/audio` gives these
  `names`.
- Session details: what the session keeps of the console at the start,
  with a download of its scene file for M32-Edit (export `scene`), and
  **Delete this session…** (`DELETE /api/sessions/{name}`), which says what
  it removes and asks first; not while it is recorded or replayed.

- Audio that fails can no longer go unnoticed (it did at a show: the
  device saved during tests was the laptop's own card, and the show was
  recorded without its audio):
  - **Start recording** asks what to do with the audio settings changed on
    the page and not saved (a device picked in Find devices but not saved
    was ignored): save them, discard them or cancel, listing what changed;
    it answers once the audio runs or has failed;
  - saving refuses more channels than the card has (`POST
    /api/audio/config` → 400), said at once under *Channels of the device*;
    Find devices tells the channels of the laptop's own card too (`arecord
    --dump-hw-params`, an instant, nothing recorded), `min_channels` for a
    range;
  - when the audio fails, *REC ⚠ NO AUDIO* in the top bar, a red bar on
    every page (amber for warnings), and on the Record page the error with
    a hint for the usual recorder errors (*Channels count non available*,
    *Device or resource busy*, a card not there) and what to do;
  - before recording, the Record page says when the device is not
    connected, is not the console's card or delivers another number of
    channels, with a button to record from the console's card
    (`GET /api/audio` → `check`); the same shows as a warning while
    recording;
  - **Start the audio** starts the audio of a recording again once the
    device is fixed, in the same session, when nothing was recorded yet
    (`POST /api/audio/restart`); **Test the input** works meanwhile;
  - the session list says *audio failed* for a session whose audio
    recorded nothing.
- Scene files: a **Scene files** tab on the Scenes page keeps `.scn` files
  in a folder of the machine running tabula (`-scenes`, default `scenes`):
  open one from the browser's computer, **Load** it into the console (every
  parameter, line by line as M32-Edit's PC → Mixer, with a progress bar and
  the lines the console does not know skipped and listed), save the
  console's settings as one, download, delete (`/api/scenefiles`, event
  `scenefile`).
- A **USB** page: the console's USB stick and player, as on its USB recorder
  screen: browse the folders, play a WAV file, pause, stop, previous and
  next, folder mode, load a scene from the stick (`/api/usb`). The stick's
  entries (up to 999) are only read when browsed.
- tabula renews its `/xremote` registration every 6 s instead of every 10 s,
  right when the console's lease ends: changes made at that moment were
  lost.
- ffmpeg is found next to tabula's own program too (`ffmpeg.exe` next to
  `tabula.exe`), and a missing recorder says how to install it; the download
  page says where to get ffmpeg for macOS and Windows.
- A **Patchbay** on the Routing page (its first view): every path a signal
  takes, drawn in seven columns in the order it goes through them, from the
  sockets it comes in by (the stage box inputs with their gain and 48 V, the
  card, the USB player) through the channels, buses, main mix, matrices and
  outputs 1-16 to the sockets it goes out by (the stage box outputs, the
  card tracks, marked when tabula records them); an effect's return loops
  back under the columns into its FX return.
  Pointing at anything lights its paths up; a tap spells them out (where it
  comes from, where it goes, with each send's tap and level) and finds any
  name or socket. Checks under the title: inputs taken from the playback
  routing, local inputs a model does not have, recorded card tracks that
  carry nothing, 48 V on an input that feeds nothing, a mix that goes
  nowhere.
- A console that does not answer when tabula connects (the laptop started
  before the console or its Wi-Fi) is asked again every few seconds instead
  of once: with `-record`, the show was not recorded at all. Connecting to
  another address replaces a connection being made.
- Disconnecting, or connecting to another console, during a recording no
  longer freezes tabula for the seconds its files take to finish: they are
  finished in the background, and the next recording waits for them.
- **Start recording** during **Test the input** waits for the test to end:
  the recording found the card busy and went on without its audio.
- A full disk (or a disk pulled out) while recording is said on the Record
  page and logged: the console's changes failed to be written without a
  word. A session with a damaged line in its change log keeps the changes
  after it (they were dropped), and the lines skipped are logged as an
  error and listed (`damaged`).
- The event stream sends a `ping` event every 5 s, so the GUI can tell a
  stream that went silent; a browser too slow to take the events is told to
  load the state again once it catches up (it stayed out of date).
- On a weak network (a code review found these): a red *Connection lost —
  reconnecting…* bar on every page, the phones' included, with the meters
  blanked instead of frozen; a stream gone silent without breaking is
  reopened after 15 s; requests give up after 10 s (longer for those that
  wait on purpose); a change refused late goes back to the console's value
  (it stayed on screen as if the console had it); a reload that fails is
  tried again, and no longer drops the changes received meanwhile nor
  blanks a fader being moved.
- Phones: **Keep the screen on** held only while Options was open; a
  musician whose bus got linked was locked out until a reload; a stereo
  in-ear mix's **My volume** and mute moved one ear when the console does
  not link bus faders; the ★ pins could move to another bus.
- **Stop recording** asks first, as do **Disconnect** and every connect
  button while a recording runs; holding M adds one marker; the USB
  page's ■ Stop asks while the console records to its stick; the Connect
  buttons stay usable while a console is being asked again.
- Phantom power asks on the Browser page too, and its dialog does what it
  says even if phantom changed meanwhile; an open graphic EQ no longer
  keeps taking the console's RTA source back; **Start recording** no
  longer asks to save audio settings that did not change; a library form
  no longer carries over from scenes to snippets; the session details say
  how many console changes are missing when lines of its log are damaged;
  mute and pan buttons are named for screen readers; the Channel page no
  longer redraws its EQ editors with every RTA frame.
- **Backup on the console's USB stick** (Record page): with it on, Start
  recording also starts the console's own recorder, a stereo WAV file of
  the main mix made by the console alone, which goes on if the laptop
  fails; Stop recording stops it. It is not started over music playing
  from the stick, a recording started on the console is left alone, and
  the page shows the file, the time and the console's dropouts, or why
  there is no backup; the session keeps the file name (`usb_backup`).
  `POST /api/record/usb`. Untested on the real M32.
- Solo, mute groups, talkback and the oscillator, reachable before only in
  the Browser: SOLO on every strip of the Mixer and **Clear solo**, the six
  mute groups next to the banks, and a hatched MUTE on strips that a mute
  group or a muted DCA silences. A new **Monitor** page: what is soloed,
  the solo modes (exclusive, PFL/AFL…), the monitor's level, source, dim
  and mono, TALK A and B (held, or latched) with their destinations, and
  the oscillator, which asks before sending its tone (to the PA, when it
  goes to the main mix).
- The Access page says whose page is open now (*Online*, with the
  device's address) or when each link was last seen, and how many links
  are online; an `online` event keeps it current. A phone gone off the
  Wi-Fi shows offline within half a minute on Linux: the GUI's connections
  close after 20 s of unanswered data, not the quarter of an hour TCP
  waits.
- A name holding double quotes (`Vox "Lead"`) no longer breaks the scene
  files tabula writes and reads (sessions, scene files): node text has no
  escape for it, so it is written with an apostrophe.
- Channel page: drag the EQ bands on the curve (sideways for the
  frequency, up and down for the gain; the wheel or Page Up/Down for the
  Q; the arrow keys), and the preamp low cut by its **LC** handle. The
  selected band is named with its values on the curve and highlighted
  below; on a phone the drawing is taller and a touch on a handle drags it
  instead of scrolling. The slope of a low cut now runs out of the drawing
  instead of lying flat along its bottom edge, as if it stopped at -18 dB.
- tabula's log goes to `tabula.log` in the sessions directory too, not only
  to the terminal, which is often gone after a show (appended; over 10 MB
  it starts again, the previous one kept as `tabula.log.1`). `-log file`
  puts it elsewhere, `-log none` keeps the terminal only. Its first line
  has the version and the command line, its last *tabula stopped*.
- The GUI says which tabula runs: the Connect page has a **This tabula**
  card (version, running since, where the log, sessions, scene files and
  access links are; `GET /api/about`), the **tabula** logo's tooltip has the
  version (`version` in the status), and a page left open while tabula was
  updated asks to be reloaded. A build from a git clone names its commit
  (`tabula -version`: `dev 9f91019`), for tabula-emu too.
- Stopping a recording and starting another within the same second failed
  (*already holds a session*): the second one is named `…-2`. A console
  change arriving while a recording starts no longer races with the
  writing of its `meta.json`.

### tabula-emu
- Recording to the USB stick: `/-stat/tape/state` 4 records and 3 pauses
  the recording; the file is named after the time
  (`/R_20261026-203512.wav`) and its time counts, but nothing is written.
- Exclusive solo (`/config/solo/exclusive`): a solo switched on turns the
  others off.
- A name holding double quotes no longer drops its node from undo, the
  library and saved scenes: node text writes the quote as an apostrophe
  and reads a console's raw quotes. A backslash in a scene name or note is
  no longer doubled.
- The debug page says when a subscription expires (`in 8s`), not `-8s ago`.
- A USB stick in the console: `-usb dir` makes a folder stand for it,
  browsed and played as with Maillot's `X32USB`: the `/-usb/dir` listing
  (entries 001… with `[..]` and `[folders]`), `/-action/recselect` enters
  folders, plays WAV files and loads `.scn`/`.snp` files, `/-stat/tape`
  state, file and elapsed/remaining time pushed every second, pause, resume
  and stop, `/-action/playtrack` previous/next, folder mode
  (`/config/tape/autoplay`) and unmounting. No sound, and no recording to
  the stick (logged as unhandled). `testdata/usb` is a small demo stick.
- Hardened against bad packets (a code review found them): one
  `/formatsubscribe` with a huge number range hung the emulator and ran it
  out of memory (ranges are now limited to 1000 numbers, 1024 parameters);
  a crafted blob crashed the OSC decoder on 32-bit builds (linux/arm), the
  tabula client's discovery included; string arguments past the end of a
  packet read as thousands of empty ones. Traffic entries and unhandled
  requests are kept to 512 bytes, unhandled kinds to 1000, a client to 64
  subscriptions, and clients idle for a minute (without subscription or
  `/xremote`) are forgotten.
- `/` text with a line feed inside (or another control character) is
  refused: it added lines to the scene files written later. Effect
  parameter text kept verbatim no longer outlives a change of one of its
  parameters.
- `/renew meters/5` (the document's own example) and `/unsubscribe
  /meters/6` find their meter subscriptions again: those meters stopped
  after 10 s.
- Sends from the FX returns to the buses reach the bus meters (a reverb in
  an in-ear bus).
- Frequencies at the top of their range read `20k00`, not `19k99`.
- Meter subscriptions served together share one computation of the mix: a
  quarter of a core with M32-Edit's subscriptions before.

### Docs
- Screenshots of tabula (mixer, channel, phone) and of the emulator's
  debug page in the README and on the site; on the site, click a
  screenshot to see it full size.
- Every screenshot is taken from `testdata/scenes/demo.scn`, a show with
  the same lineup as the real one and made-up performer names, which the
  quick start and `make dev` now load too.
- The tests use only scenes with made-up names: `demo.scn`, `vocals.scn`
  and `vocals-live.scn` (real shows, renamed); the real scenes left the
  repository. The docs say how to put your own scenes in the emulator's
  library (`-library var/library`, files `scene-NN.scn`).

## v0.2.0 — access for the band, audio recording

### tabula
- Replays can run at 3× (besides 0.5×, 1×, 2×, 4× and 8×); docs/recording.md
  explains how a session keeps the time of each change and how a replay
  follows it without drifting (tested to within a millisecond).
- The Record page says what a session keeps: the console (its settings and
  every change, not the sound) always, the audio only when Audio tracks is
  on; the session list shows which sessions have audio, and replays say
  they move the console and play no sound.
- Security: requests that change something are refused when they come from
  a page of another site (a page open in a browser of the laptop could
  otherwise use tabula as the engineer, and set a custom recorder command),
  and tabula answers only to IP addresses, localhost and the machine's name
  (`-allowed-hosts` adds others), against DNS rebinding.
- The musicians' in-ear page is a desk of tall vertical faders with meters,
  one per source with its name and colour, the musician's own volume fixed
  on the right, and an overview bar at the bottom to slide through the
  sources without touching a fader. Linked channels are one fader.
- The Browser page is a tree of the OSC paths (`ch` → `01` → `eq` → `1`)
  instead of one long list per section, with the strip names, the current
  values on every row and subtree counts; a filter opens the tree down to
  its matches.
- An optional engineer password, set on the Access page: then every
  browser signs in to be the engineer, the machine running tabula included
  (without one, anyone at that machine, or every device behind a reverse
  proxy on it, has full control). Signed-in devices are listed and can be
  signed out; changing the password signs out the others; wrong passwords
  are slowed down; it is kept as a PBKDF2 hash; `tabula -reset-password`
  removes it. Musician and viewer links work as before.
- Access for the band: the engineer makes access links (with QR codes) on a
  new Access page. A musician's link opens their in-ear mix only, on a page
  made for phones: their bus volume and mute, a send slider per source,
  pins, stereo pan for linked buses. Viewer links see everything read-only.
  The machine running tabula stays the engineer; other devices without a
  link see nothing (`-guest` changes that). The server checks every write,
  musicians change their mix without arming, and deleting a link ends it
  at once. See [access](docs/access.md).
- Changes are recorded with who made them through a link (`web Theo`).
- Records the audio of the console's USB card with each session, in the
  same program, so the tracks line up with the console moves: one WAV file
  per track (24-bit by default), named after what feeds it on the console
  (`01 Hang.wav`), in the session's `audio/` folder. Choose the device, the
  tracks (`1-16`) and the format on the Record page, test the input with
  its levels, and watch the capture, the free space and the time left
  while recording; or use `-audio-device`, `-audio-tracks`… with `-record`.
  The files stay readable if the program dies, become RF64 past 4 GiB, and
  the audio stops cleanly before the disk is full; a failing capture never
  stops the session. The time of the first sample and the clock drift are
  measured and kept in `meta.json`. Records through arecord, parecord or
  ffmpeg (no driver, no build option). See [recording](docs/recording.md).
- Sessions export as a Reaper project (tracks at their place, markers), an
  Audacity label track and a CSV of every change and marker; a recording
  with audio leaves the project and the labels in its folder.
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
- New Scenes page: the console's scenes, snippets and channel, effect,
  routing and AES50 presets; recall after a confirmation, save (asking
  before replacing), rename, copy, delete; channel presets load onto a strip
  with a choice of sections, effect presets into an FX slot. New API
  `POST /api/library`, which reads back what each command changed;
  `console.Client` gained `Library` and `Refresh`.
- Mixer: linked pairs of channels, aux inputs, FX returns, buses and
  matrices show as one stereo strip (both meters, one fader and mute, written
  to the odd strip, which the console mirrors) when their faders and mutes
  are linked. The Channel page says which strip a strip is linked with.
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
