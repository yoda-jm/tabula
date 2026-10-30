# tabula client HTTP API

`tabula` serves its GUI and this API on one HTTP port (default
`localhost:8033`). It holds at most one console connection, shared by every
browser. JSON everywhere; `/api/schema` and `/api/state` are gzipped when the
client accepts it.

## Connection

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/status` | | `Status` |
| GET | `/api/about` | | `About` |
| GET | `/api/discover` | | consoles answering a broadcast `/xinfo` within 1.5 s: `[{ip, name, model, firmware}]` |
| POST | `/api/connect` | `{"addr": "192.168.1.62"}` (port 10023 by default) | 202 + `Status`; connection and sync go on in the background, follow `status` events. A console that does not answer is asked again (every 1 s, then up to every 10 s) until it does or another address is chosen: `connecting` stays true, `error` says why. A recording running ends (its files are finished in the background) |
| POST | `/api/disconnect` | | `Status`; a recording running ends |
| POST | `/api/arm` | `{"armed": true}` | `Status`; writes are refused (403) until armed. Disarming while a scene file loads answers 202, still armed: the file is sent to its end, then writes disarm (its `SceneProgress` says `"disarm_after": true`); arming again before the end keeps them armed |

`Status`:

```json
{
  "connected": true, "connecting": false, "error": "",
  "console": {
    "addr": "127.0.0.1:10023",
    "info": {"server_version": "V2.07", "server_name": "osc-server", "model": "M32C",
             "firmware": "4.06", "ip": "127.0.0.1", "name": "tabula-emu"},
    "connected": true, "synced": 3243, "total": 3243, "missing": [],
    "armed": false, "last_seen": "2026-10-17T12:36:52Z",
    "link": {"rtt_ms": 3.4, "asked": 100, "lost": 0, "caught": 1,
             "last_caught": "/ch/05/mix/fader  -10.2", "caught_at": "2026-10-27T21:14:05Z"}
  }
}
```

`connecting` stays true until the initial sync is over (`synced` reaches `total`).

`link` is measured by the read-back: while connected, tabula asks the
console for every node again, 25 a second (the mix nodes every other
time). `rtt_ms` is the smoothed round trip, `lost` how many of the last
`asked` (up to 100) went unanswered within a second, `caught` how many
values were found changed since the connection: changes lost on the
network, applied with the origin `console, read back` (in `change` events
and in sessions) and logged. Nothing is written to the console. A `status`
event is sent when the link's figures changed, every 5 s at most.

`Status` also has `version`, tabula's version (`v0.2.0`, or `dev 9f91019`
for a build from a git clone, `+` when it holds changes not committed): a
page that sees it change was opened before tabula was updated.

`About` (the engineer's) is which tabula runs and where it keeps its files,
as absolute paths: `{"version", "started", "log", "sessions", "scenes",
"access", "listen"}`; `log` is `""` when the log goes to the terminal only
(`-log none`), `access` when the access links are kept in memory only.

## Schema

`GET /api/schema` → `{"model", "types": [Type…], "nodes": [Node…]}`, in scene
file order then status nodes.

- `Node`: `{"path": "/ch/01/mix", "scene": true, "variant": "/fx/1/type"?, "fields": [{"name": "fader", "label"?, "type": 12}]}`.
  A parameter's address is `path + "/" + name`. `type` indexes `types`.
- `Type`: `{"kind", "describe", "names"?, "min"?, "max"?, "steps"?, "bits"?, "unit"?}` with kind:
  - `enum`: value is the index in `names`;
  - `int`: `min`…`max`;
  - `bitmap`: `bits` bits (bit 0 = rightmost digit of the text);
  - `string`: up to `max` characters;
  - `lin`, `log`: float normalized to [0, 1] on the wire; displayed value is
    `min + v·(max−min)` (lin) or `min·(max/min)^v` (log); `steps` known values
    (`round(v·(steps−1))/(steps−1)`), 0 = continuous;
  - `level`: fader law, [0, 1] ↔ dB: 0 = −∞; v < 0.0625: 480v − 90;
    v < 0.25: 160v − 70; v < 0.5: 80v − 50; else 40v − 30 (0.75 = 0 dB, 1 = +10 dB);
  - `unused` (effect positions not used), `raw` (kept as text).
- Variant nodes (`variant` set: effect parameters `/fx/N/par`) change their
  fields with the selector (the effect type): read them from `/api/state`.

## State

`GET /api/state?prefix=/ch/01/` → `[{"path", "line", "values": [{"addr", "text", "value"}], "fields"?, "text"?}]`

- `line` is the node text as the console writes it; `text` is one field's
  token (e.g. `" -6.0"`, `"GATE"`, `"\"Vx Theo\""`); `value` the raw value
  (normalized float, int index, or string).
- Variant nodes carry their current `fields` with inlined `type_info`.
- `"text": true` means the node is kept as text (an effect without a
  parameter table yet): only `line` is meaningful.

## Writes

`POST /api/set` with `{"addr": "/ch/01/mix/fader", "text": "-6"}` (node text,
as typed on the console: `-6`, `GATE`, `1k20`…) or
`{"addr": "...", "value": 0.75 | 3 | "name"}` (floats normalized, enums by
index or name). 204 on success, 403 if writes are not armed (or the
caller's access does not allow it, see [Access](#access)), 400 otherwise.
The value is quantized like the console does; the resulting change comes
back on the event stream.

## Library

The console's scenes, snippets and presets. Their listings are status
nodes, read with `/api/state` like any other:
`/-show/showfile/scene/000`…`099` and `/-show/showfile/snippet/000`…`099`
(slot 0-99), `/-libs/ch/001`…`100`, `/-libs/fx/…`, `/-libs/r/…`,
`/-libs/mon/…` (channel, effect, routing and AES50/DP48 presets, slot N is
`N+1`). Each has a `name` and `hasdata` (1 when the slot holds something);
scenes also have `notes`.

What a recall changes is set with `/api/set` like any parameter (armed): a
scene's `safes` (bits 1-8), a snippet's filters (`eventtyp`, `channels`,
`auxbuses`, `maingrps`), the safes of every recall in
`/-show/showfile/show` (`inputs`, `mxsends`, `mxbuses`, `console`,
`chan16`, `chan32`, `return`, `buses`, `lrmtxdca`, `effects`) and
`/-prefs/safe_masterlevels`. The bits are described in
[emulator.md](emulator.md#scene-safes-and-snippet-filters).

`POST /api/library` sends one command to the console (engineer only, writes
armed):

| Field | Meaning |
|---|---|
| `op` | `save`, `load` (recall), `copy`, `delete`, `rename` |
| `kind` | `scene`, `snippet`, `libchan`, `libfx`, `librout`, `libmon` |
| `index` | slot 0-99 |
| `name` | `save`, `rename` |
| `note` | `save` of a scene (optional) |
| `to` | `copy`: destination slot (snippets cannot be copied) |
| `target` | `libchan`: strip index 0-71 (channels 1-32, aux ins, FX returns, buses, matrices, LR, M/C) saved from or loaded to; `libfx`: FX slot 0-7 |
| `scope` | `load` of a `libchan`: what to load, bits 0 headamp, 1 configuration, 2 gate, 3 compressor, 4 EQ, 5 sends (default 63, all) |

Examples: `{"op": "load", "kind": "scene", "index": 3}`,
`{"op": "save", "kind": "libchan", "index": 45, "name": "Vox", "target": 11}`.

Answers: 204 when the console answered with success; 400 for a malformed
command; 403 unless writes are armed; 409 when not connected; 422 when the
console refused it (an empty slot, for instance); 504 when it did not answer
(it may have done it all the same). After a success, tabula reads again what
the command changed: the listing before answering, or, for a recall, every
value it may have changed, in the background — the changes come on the
event stream.

## Scene files

The `.scn` files kept in a folder of the machine running tabula
(`-scenes`, default `scenes`), as the console, M32-Edit and X32-Edit
write them. Engineer only.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/scenefiles` | | `{"dir", "files": [{"file", "name", "note"?, "size", "modified"}], "loading"?, "backup_error"?}`: `name` and `note` from the file's first line (`#4.0# "name" "note" …`); `loading` while one is being sent; `backup_error` when the last backup (below) failed |
| GET | `/api/scenefiles/{file}` | | the file, as a download; 404 |
| PUT | `/api/scenefiles/{file}` | the file's text | 204: kept in the folder (`?replace=1` to replace one of the same name, else 409); 400 for a name that is not `something.scn` or a text without parameter lines; 413 above 2 MB |
| DELETE | `/api/scenefiles/{file}` | | 204; 404 |
| POST | `/api/scenefiles` | `{"name", "note"?, "file"?, "replace"?}` | `{"file"}`: the console's current settings written as a scene file, named `<name>.scn` unless `file` says; 409 when the name is taken (without `replace`) or when not connected |
| POST | `/api/scenefiles/{file}/load` | | `SceneProgress`, once sent: the file sent to the console, line by line with the `/` command (each echoed before the next goes, as the editors' PC → Mixer). Every parameter it holds changes at once, without the console's scene safes. Lines this console's schema does not know are skipped and listed. 403 unless writes are armed; 409 when not connected, while a replay runs or another file loads; 404 |

`SceneProgress`: `{"file", "sent", "total", "done", "skipped"?: [line],
"error"?, "disarm_after"?, "backup"?}` (`backup`: the file the console's
settings were saved as before the load), also sent as `scenefile` events
while a file loads (every 50 lines) and when it is done. A load goes on if
the browser leaves.

**Backups.** Before anything overwrites the console's settings, they are
saved as `Backup YYYY-MM-DD hh.mm.ss.scn` in this folder: a scene file load,
`POST /api/replay/start`, a library `load` (`POST /api/library`), opening a
scene, snippet, show or preset file of the USB stick, and `POST /api/set` of
`/-action/goscene`, `gosnippet`, `gocue`, `initall 1` or `doundo 1`, armed.
The newest 30 are kept; settings equal to the newest backup are not saved
again. A backup that fails does not stop the command: it is logged, and
`GET /api/scenefiles` says `"backup_error"` until one works again.

## USB stick

The USB stick in the console, as on its USB recorder screen (the commands
of Maillot's X32USB). Engineer only.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/usb` | | `UsbView`, read from the console (it does not push its listing); 409 when not connected, 504 when it does not answer |
| POST | `/api/usb` | `{"op": "open", "index"}` | `UsbView` after it: `/-action/recselect index` enters a directory (the listing is read again once the console changed `/-usb/path`, 1.5 s at most), plays a WAV file, or loads a scene, snippet, preset or show file (the console's values it may have changed are then read again, as after a recall; the show's listings after a show); 400 for an entry not in the directory, 409 without a stick |
| POST | `/api/usb` | `{"op": "play"}`, or `pause`, `stop`, `ff`, `rew`, `prev`, `next` | 204: `/-stat/tape/state` 2, 1, 0, 5 (fast forward), 6 (rewind), or `/-action/playtrack` -1, 1. The player winds until `play`, `pause` or `stop` |

Commands need armed writes (403). `UsbView`: `{"mounted", "path" (the
current directory's name, "" at the root), "title" (the file selected),
"pos", "entries": [{"index", "name", "kind"}]}`, `index` as `recselect`
numbers it (from 1), `name` without the brackets of directories, `kind`
`parent`, `dir`, `wav`, `scene`, `snippet`, `show`, `effect`, `routing`,
`channel`, `monitor` (AES50/DP48 presets, `.mon`), `prefs`, `volume` or
`file`. The player's state is in the
mirror, pushed by the console: `/-stat/tape` (`state`, `file`, `etime`,
`rtime` in seconds), `/config/tape/autoplay` (play the folder). The
entries `/-usb/dir/001`…`999` are lazy nodes: not read when connecting.

## Events

`GET /api/events` — Server-Sent Events:

- `status`: a `Status` (sent on connect and on every state change of the connection);
- `change`: `[{"addr", "text", "value", "origin"}]` with origin `console`
  (made on the console or by another remote app), `local` (made through
  this server from its machine) or `web <name>` (through an access link);
- `access`: the musicians' permission changed — read `/api/whoami` again;
- `online`: which devices have a page open (`Online`, see Access), each
  time an event stream opens or closes; to engineers only;
- `resync`: the initial sync finished, or this stream dropped events (a
  browser too slow to take them) — reload `/api/state`;
- `record`: a `Record` (recording, replay, audio);
- `scenefile`: a `SceneProgress` while a scene file is sent to the console;
- `undo`: an `UndoView` when what Undo would undo changed, to engineers only;
- `setlist`: the `Setlist` when it changed (saved, a song marked, moved), to engineers only;
- `meters`: `{"id": "1", "values": [...]}` for the meter groups named when
  opening the stream, e.g. `/api/events?meters=1,2,6:12` (`N` is
  `/meters/N`, `N:a:b` adds its parameters: `5:0:0` channels 1-16 + DCAs,
  `6:12` strip 13). Values are linear levels (1.0 = 0 dBFS; gain reductions
  are gains, 1.0 = none), dB for group 15 (RTA, 100 bands), gains for group
  16. The groups of a stream are fixed: to change them, open another
  stream and close the first once the second has sent its `status` (the
  GUI does so, missing no change). About 20 updates per second per group;
  see the protocol document for what each group holds (e.g. 1: 32 inputs,
  32 gate and 32 compressor gains; 2: 16 buses, 6 matrices, main L/R, mono,
  then their compressor gains). Every page gets 0 and 2; the GUI adds 1
  (the channels' gain reductions) and `5:0:0` (the DCAs) while a page
  shows them, 15 for the RTA.
- `ping`: `{}` every 5 s, so a browser can tell a stream gone silent (a
  connection cut without a word, as when a phone changes Wi-Fi) and open
  it again; the GUI does so after 15 s without any event.

## Access

Requests that change something are refused (403) when a browser says they
come from a page of another site (`Sec-Fetch-Site`, or `Origin` not matching
the host); requests without those headers (scripts, `curl`) are not
concerned. Every request naming `tabula` by a host name it does not know is
refused too: IP addresses, `localhost` and the machine's name work, other
names need `-allowed-hosts`.


Every API request is checked against the caller's role (see
[access](access.md)): requests from the machine running `tabula` are the
engineer's, unless an engineer password is set; other devices use an access
link, which sets a cookie (`tabula_access`), or get the guest role
(`-guest`, none by default). With a password, every browser (this machine
included) signs in with `POST /api/login`, which sets the same cookie.

| Least role | Routes |
|---|---|
| none | `GET /api/whoami`, `POST /api/login`, `POST /api/logout`, `GET /a/{token}` |
| viewer | `GET /api/status`, `/api/schema`, `/api/state`, `/api/events`, `/api/record` |
| musician | `POST /api/set`, only for the sends to their bus (and its linked partner) from channels, aux inputs and FX returns (`on`, `level`, `pan`), and their bus `fader` and `on`; allowed without arming when the musicians may change their mix |
| engineer | everything else, including any route not listed here |

A refused request gets 403 with `{"error": "..."}`.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/a/{token}` | | sets the cookie and redirects to `/#/mix` (musician) or `/#/mixer`; 403 page if the link is not valid |
| GET | `/api/whoami` | | `{"role", "name"?, "bus"?, "buses"?, "local", "link", "session", "password", "musicians_can_write"}` |
| POST | `/api/login` | `{"password": "..."}` | signs in with the engineer password: makes a session, sets the cookie, answers the new `whoami`; 403 wrong password, 429 too many wrong ones (with `Retry-After`), 409 no password set |
| POST | `/api/logout` | | forgets the cookie (and ends a session); answers the new `whoami` |
| GET | `/api/access` | | `{"grants": [Grant], "musicians_can_write", "password", "current"?, "guest", "urls", "name_url"?, "reachable", "online": Online}`; `name_url` (`http://actarus.local:8033`) when tabula listens on all its addresses and a multicast DNS query for this machine's `.local` name is answered with one of its addresses (checked again after a minute, 15 s after no answer) |
| POST | `/api/access` | `{"name": "Theo", "role": "musician", "bus": 3}` | 201 + `Grant` |
| DELETE | `/api/access/{id}` | | 204; event streams opened with it end |
| POST | `/api/access/settings` | `{"musicians_can_write": false}` | the access status |
| POST | `/api/access/password` | `{"current": "...", "password": "..."}` | sets, changes or (empty `password`) removes the engineer password; `current` is needed when one is set; ends every other session; setting the first one signs the caller in; answers the access status (400 under 8 characters, 403 wrong `current`, 409 with `-guest engineer`) |

`Grant`: `{"id", "name", "role", "bus"?, "session"?, "token", "created"}`;
its link is one of `urls` + `/a/` + `token`. Sessions (`"session": true`,
browsers signed in with the password) come without their token and are
ended with `DELETE /api/access/{id}` like links; `current` is the access of
the caller. `urls` lists where other devices reach
this `tabula` (one per address of the machine when it listens on all of
them); `reachable` is false when it listens on the machine only.

`Online`: `{"grants": {id: {"streams", "addr"?, "seen"}}, "guests":
[address]}` — for each access used since `tabula` started, how many event
streams are open with it now (0: offline), the IP address of the last one
and when it last opened or closed one; `guests` are the addresses of the
devices with a stream open without an access (not this machine). On Linux
the GUI's connections close once their data goes unanswered for 20 s
(`TCP_USER_TIMEOUT`), so a phone gone off the Wi-Fi goes offline within
half a minute.

`login` and a wrong `current` password count as tries: after 5 wrong
passwords in a row a client waits 30 s, twice as long after each further
one (up to an hour), and all clients together get 30 wrong passwords a
minute at most. The password is kept as a PBKDF2-SHA256 hash in the access
file; `tabula -reset-password` removes it.

## Recording and replay

Sessions are directories under `-sessions` (default `./sessions`), named by
their start time (`2026-10-17_203000`, `2026-10-17_203000-2` for a second
recording started within the same second): `meta.json`, `start.scn` (the
snapshot, a regular scene file), `start.nodes` (other nodes), `events.jsonl`
(one change or marker per line: `{"t": seconds, "addr", "text", "value",
"origin"}` or `{"t", "marker"}`).

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/record` | | `Record` |
| GET | `/api/check` | `?hours=3` | `{"lines": [CheckLine], "hours", "at"}`: the pre-show check for a show of `hours` (3 by default, 0.25 to 24). `CheckLine`: `{"id", "title", "level", "text", "fix"?}`, `id` one of `console`, `link` (when connected), `card`, `rate`, `device`, `disk`, `power`, `sleep`, `usb`, `phones`, `busy`; `level` `ok`, `warn`, `bad`, `unknown` (this machine cannot tell) or `info` (nothing to check). What is slow to find (power, sleep, the audio devices) is kept 5 s. Engineer only |
| POST | `/api/record/start` | `{"note": "set 1"}` | `Record` (409 if not connected or already recording). With audio, it answers once the audio delivers samples or has failed (3 s at most), so `audio.state` says whether it runs |
| POST | `/api/record/mark` | `{"label": "Song 2"}`, or `{"next": true}` (the next song of the setlist, then the next one moves on), or `{"song": 2}` (that song of the setlist, from 0; the next one is the one after it); with none of them, or when no song is left: `Marker 12m34s`, the time since the start | `Record` and `"marked": {"label", "t"}`, the marker added (`t`: seconds after the start); 409 when not recording, 400 for a song not in the setlist |
| POST | `/api/record/stop` | | `Record` |
| POST | `/api/record/usb` | `{"on": true}` | `Record`: turns the backup on the console's USB stick on or off for the next recordings (kept in `audio.json` as `usb_backup`, which `POST /api/audio/config` leaves as it is); turned on during a recording, it starts now |
| GET | `/api/sessions` | | `[{"name", "meta"}]`, newest first |
| GET | `/api/sessions/{name}` | | `{"name", "meta", "markers": [Event], "snapshot", "scene", "size", "flac_size"?, "setlist"?}` (`setlist`: the songs the session was recorded with): `snapshot` counts the node lines of the console at the start (`start.scn` and `start.nodes`), `scene` says whether `start.scn` is there; `size` is the bytes of the session's folder, `flac_size` about what it takes with its WAV tracks in FLAC (see [Taking a show home](#taking-a-show-home)) |
| DELETE | `/api/sessions/{name}` | | 204: the session folder is removed, audio included (not `audio.json`); 409 while it is recorded or replayed |
| POST | `/api/replay/start` | `{"session": name, "from": seconds, "speed": 1, "audio"?: true, "routing"?: true}` | 202 + `Record`; 403 unless writes are armed (a replay writes to the console). With `audio`, a virtual soundcheck (below): 409 for a session without audio tracks, while recording, while the input is tested, or when no player can be run; 400 for a speed other than 1; with `routing` too, 502 when the routing cannot be switched |
| POST | `/api/replay/stop` | | `Record` |
| GET | `/api/undo` | | `UndoView`: `{"count", "what"?, "at"?}`, what Undo takes back (the engineer's last change made from tabula) |
| POST | `/api/undo` | | `UndoView` after undoing the last step: the values it overwrote are sent back to the console; 403 unless writes are armed; 409 when there is nothing to undo; 502 when the console does not answer (a step not started stays) |
| POST | `/api/strips/copy` | `{"from": "/ch/01", "to": ["/ch/02"], "sections": ["eq", "dyn"]}` | `{"nodes"}`: the sections' nodes (`preamp`, `delay`, `gate`, `dyn`, `insert`, `eq`, `sends`: `mix/01`…, `main`: `mix`) sent to each target with the `/` command; one undo step. Strips of the same kind only (`ch`, `auxin`, `fxrtn`, `bus`, `mtx`); 400 otherwise or when none has the sections; 403 unless armed |

**Setlist** (engineer only): the show's songs and the next one to mark,
kept in `setlist.json` in the sessions folder. `Setlist`: `{"songs":
[...], "next", "updated"}`, `next` from 0, `songs.length` once all are
marked. It is sent as `setlist` events to the engineers when it changes.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/setlist` | | `Setlist` |
| PUT | `/api/setlist` | `{"songs": [...], "next"?: n}` | `Setlist`: the songs replaced (spaces tidied, empty ones dropped; 200 songs of 80 characters at most, else 400); without `next`, the next song stays the same song wherever it moved |
| POST | `/api/setlist/next` | `{"next": n}` | `Setlist`: song `n` is the next one (back one song, skip one) |

**Patch list** (engineer only): what the Patch list page prints that the
console does not know, kept in `patchlist.json` in the scenes folder. The
page makes the PDF itself from the console's routing. `PatchList`:
`{"title", "details", "contact", "box_a", "box_b", "paper": "A4" | "Letter",
"watermark", "notes": {row: note}, "rows": {row: true | false}, "updated"}`.
`box_a` and `box_b` name the stage boxes, `""` for the one the console
reports. `notes` holds a note per row and `rows` the rows ticked or
unticked by hand; the others follow their default. A row is a strip
(`/ch/01`, `/auxin/03`), a box output (`AES50A:13`), an XLR output (`OUT:5`)
or an output node (`/outputs/p16/02`).

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/patchlist` | | `PatchList`; before anything is saved: no title, A4, the watermark |
| PUT | `/api/patchlist` | `PatchList` | `PatchList`: replaced, spaces tidied, empty notes dropped; what the body leaves out takes its first value. 400 for texts over 120 characters, notes over 80, a paper other than A4 or Letter, a row key with spaces or over 40 bytes, more than 400 notes or rows |

`Record`: `{"recording", "name"?, "meta"?: {"start", "events", "markers", "duration", "audio"?, "usb_backup"?, …}, "replaying", "replay_of"?, "replay_at"?, "replay_audio"?, "error"?, "waiting"?, "audio"?, "usb"}`; `waiting` counts the console changes waiting for a slow disk for more than 2 s.
`usb` is the backup on the console's USB stick: `{"on" (the setting),
"starting"?, "file"?, "already"?, "error"?}` for the recording, or the last
one — the file the console records (`/R_20261026-203512.wav`), `already`
when the console was recording before (Stop recording leaves it on), or
why there is no backup (no stick, the player in use, the console not
starting). `meta.usb_backup` keeps `{"file"?, "already"?, "error"?}` in the
session. With the setting on, starting a recording sets
`/-stat/tape/state` to RECORD (4) without writes armed, unless the
console's player plays or is paused, and stopping it (Stop, Disconnect,
`tabula` stopping) sets it to STOP (0), unless the player was used since.
A session's `damaged`: the lines of its `events.jsonl` that do not read (their
changes are skipped, and logged as an error).
`audio` is the state of the audio capture of the recording (or of the last
one), an `AudioStatus` without `device` and `command` (viewers see the
record status); `meta.audio` the audio section of the session (see Audio).
A replay first sends the state at `from` (snapshot plus the changes up to
then), then each later change at its time divided by `speed` (any value
above 0; 1 is real time), counted from the start of the replay.

A **virtual soundcheck** (`"audio": true`) also plays the session's tracks
to the card, at 1×: they start with the timed changes, from where `from`
falls in the audio (silence before its first sample), each on its recorded
channel, streamed to the player of the audio settings
(`AudioView.play_command`), and play to their end unless stopped;
`replay_audio` is true meanwhile, and `replay_at` moves with the audio. The
replay leaves `/config/routing/routswitch` alone; with `"routing": true`,
tabula sets it to PLAY (1) before and back to what it was after (even
unarmed), so the inputs hear the card through `/config/routing/PLAY`. A
player that fails ends the replay with its message in `error`. A backup of
the console's settings is saved first, as before every replay.

Events: `record` carries a `Record` (every second while recording, and on
every replay progress).

`tabula -record -console <ip>` starts recording at once when the audio
tracks are on (the audio alone until the console is synced, then the
console joins the session), else as soon as the console is synced (asking
it again until it answers), and stops on exit — for unattended recording
of a concert. `POST /api/record/start` without a console connected records
the audio alone when the audio tracks are on (409 otherwise): `Record` has
`"no_console": true` until the console joins (its snapshot of then, its
changes from then on, `meta.console_from` its time, a `console connected`
marker); `Record.audio_on` says whether the audio tracks are on. `POST /api/record/start` waits (5 s at
most) for an input test running, or a recording ended by a disconnect, to
free the card; 409 if it does not. `Record.error` says when the console's
changes cannot be written (a full disk): those after it are lost.

## Taking a show home

A whole session, downloaded as one zip or copied to a disk plugged into
the machine running tabula (a USB disk), its WAV tracks as they are or
converted to FLAC by ffmpeg on the way. Nothing is written on that
machine's disk. Engineer only; the session being recorded is refused (409:
its files are complete once it stops), and so is FLAC without ffmpeg (409).
See [recording](recording.md#taking-a-show-home).

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/sessions/{name}/zip` (`?flac=1`) | | a download, `<name>.zip` (`<name> FLAC.zip`), made while it is sent: the folder's files under `<name>/` (`<name> FLAC/`), the small ones first, the tracks stored (not compressed), the others deflated. With FLAC, `audio/*.wav` become `audio/*.flac` and `<name>.rpp` points at them. A failure cuts the download (logged) |
| GET | `/api/disks` | | `{"disks": [{"path", "name", "free", "size", "writable"}], "flac"}`: the disks a session can be copied to (Linux: mounted under `/media`, `/run/media`, `/mnt`; macOS: `/Volumes`; Windows: the other drives), not the one holding the sessions; `free` and `size` in bytes (-1 unknown); `flac` says ffmpeg is there |
| POST | `/api/sessions/{name}/copy` | `{"disk": path, "flac": false}` | 202 + `Copy`: copies the session into `<disk>/tabula/<name>` (`<name> FLAC`), through `<name> (copying)`, renamed once complete. 400 for a disk not listed, 409 when read-only, when the session is already there, or while another copy runs; 507 when the disk has less room than the session (the FLAC estimate) plus 16 MB |
| GET | `/api/copy` | | `Copy`, the copy running or the last one (`null` before any) |
| POST | `/api/copy/stop` | | `Copy`: cancels the copy running; what it copied is removed |

`Copy`: `{"session", "disk", "folder" (where it is once complete), "flac",
"done", "total" (bytes read from the sessions' disk), "files", "of",
"file"? (being copied), "state": "copying"|"done"|"failed"|"cancelled",
"error"?, "recording"? (started while a show was being recorded),
"started", "ended"?}`. A copy that does not end `done` leaves nothing on
the disk.

## Audio

When audio is enabled, starting a recording also records the tracks of the
console's USB card into the session's `audio/` directory, one WAV file per
track, and stopping the recording stops it (see
[recording](recording.md)). An audio capture that fails is reported
(`Record.audio.state` `failed`, `error`, with a hint for the usual recorder
errors) and the recording of the changes goes on; `POST /api/audio/restart`
starts it again once fixed. Engineer only.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/audio` | | `AudioView` |
| POST | `/api/audio/config` | `AudioConfig` | `AudioView`; 400 if not valid, or when the device is connected and has fewer input channels than `channels` (32 channels from a card with 2 inputs: the message says what to set, and names the console's card if connected). Used from the next recording, saved in `<sessions>/audio.json` |
| GET | `/api/audio/devices` | | `[{"recorder", "id", "name", "channels"?, "min_channels"?, "format"?, "rates"?, "console"?}]`: what the recorders can record from (`id` is the device to use); `console` marks what looks like the console's card. `channels` is the most the device records (`min_channels` the fewest, when it takes a range): from `/proc/asound` for USB cards, PipeWire for sources, and for the other ALSA cards (an HDA card) from `arecord --dump-hw-params`, which opens the card for an instant and records nothing; not while the input is recording or being tested. Saving the settings asks the same way |
| POST | `/api/audio/test` | `AudioConfig` (optional: default the saved one) | `AudioStatus` after 3 s of capture, nothing written: the stream format, `peaks`, or `error`; 409 while the audio is being recorded (allowed once it failed) |
| POST | `/api/audio/restart` | | `Record`, once the audio delivers samples or has failed (3 s at most): starts the audio of the recording again with the saved settings, in the same session (its empty files replaced, the first sample timed from the session start). Only before any audio was recorded: 409 when the audio runs, when the session already has audio (stop and start a new recording), when not recording or while testing; 400 when audio is off |
| GET | `/api/sessions/{name}/export?format=` | | a download: `reaper` (`<name>.rpp`), `audacity` (`<name> labels.txt`: `start⇥end⇥label` per marker, in seconds of the audio files), `csv` (`<name>.csv`: `time,audio_time,clock,type,address,text,value,origin,label`), `scene` (`<name>.scn`: `start.scn`, the console at the start, for M32-Edit; 404 without it) |

`AudioConfig`: `{"enabled", "recorder": "arecord"|"parecord"|"ffmpeg"|"custom",
"device", "channels": 32, "rate": 48000, "tracks"?: "1-16", "bits": 24,
"plain_names"?, "command"?, "input"?, "play_to"?, "play_command"?}` —
`command` is the custom recorder's command line (`{device}`, `{channels}`,
`{rate}` replaced), writing raw samples in the `input` format (`s16le`,
`s24le`, `s24-32le`, `s32le`, `f32le`) or a WAV stream. `play_to` is where
a virtual soundcheck plays the tracks (default: the device recorded from),
`play_command` a player replacing the recorder's (reading raw `s32le` on
its standard input); a new `play_command` is refused (403) unless chosen on
the machine running tabula, as a custom recorder.

`AudioView`: `{"config", "command"? (what the configuration runs), "error"?
(what is wrong with it), "templates": {recorder: command}, "byte_rate",
"dir", "free", "remaining", "status"?: AudioStatus, "testing", "names"?,
"check"?, "play_command"?, "play_error"?, "play_device"?}` — `play_command`
is how a virtual soundcheck plays the tracks (or `play_error` why it
cannot), `play_device` where they go; `free`
is the free space in the sessions directory (bytes, -1 unknown),
`remaining` how many seconds of audio it holds at `byte_rate` (the chosen
tracks only), `names` what the console's routing sends to each of the 32
tracks of its card (`""` when nothing named), while connected. `check`,
when audio is on and something is wrong with the device: `{"problems":
[text], "console"?: device}` — the device is not connected, is not the
console's card, or delivers another number of channels (Linux: ALSA devices
and PipeWire/PulseAudio sources; with `plain_names`, only a missing device
counts); `console` is the console's card, connected, to record from
instead (a device as `/api/audio/devices` lists them).

`AudioInfo` (the `audio` section of a session's `meta.json`): `{"rate",
"bits", "channels", "tracks": [{"channel", "name"?, "file"}], "offset",
"start", "drift_ppm"?, "frames", "device"?, "command"?, "overruns"?,
"gaps"?: [{"at", "seconds", "why"?}], "error"?}`. `gaps` are where the audio
stopped mid-recording and the recorder was started again: silence in the
files from `at` (seconds into the files) for `seconds`. `file` is relative to the session; `offset` is the time of the
first sample in seconds after the session start; a session time `t` is at
`(t − offset) / (1 + drift_ppm·10⁻⁶)` in the files.

`AudioStatus`: an `AudioInfo` plus `{"state": "starting"|"recording"|"recovering"|"stopped"|"failed",
"warnings"?, "format"?, "seconds", "bytes", "byte_rate", "free",
"remaining", "peaks"?, "silent"?}`: `recovering` while the recorder is
started again after the audio stopped mid-recording; `silent` is how long
every track has been below -80 dBFS (seconds; a warning from 30 s); `remaining` is the audio the disk still holds
before the capture stops itself (seconds), `peaks` the peak level of each
track over the last second (linear, 1 = full scale). Warnings: the device
is not the console's card (the `check` problems, found when the capture
starts), the disk is almost full (under 20 minutes), the recorder lost
samples, no audio for a while.
