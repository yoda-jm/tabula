# tabula client HTTP API

`tabula` serves its GUI and this API on one HTTP port (default
`localhost:8033`). It holds at most one console connection, shared by every
browser. JSON everywhere; `/api/schema` and `/api/state` are gzipped when the
client accepts it.

## Connection

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/status` | | `Status` |
| GET | `/api/discover` | | consoles answering a broadcast `/xinfo` within 1.5 s: `[{ip, name, model, firmware}]` |
| POST | `/api/connect` | `{"addr": "192.168.1.62"}` (port 10023 by default) | 202 + `Status`; connection and sync go on in the background, follow `status` events |
| POST | `/api/disconnect` | | `Status` |
| POST | `/api/arm` | `{"armed": true}` | `Status`; writes are refused (403) until armed |

`Status`:

```json
{
  "connected": true, "connecting": false, "error": "",
  "console": {
    "addr": "127.0.0.1:10023",
    "info": {"server_version": "V2.07", "server_name": "osc-server", "model": "M32C",
             "firmware": "4.06", "ip": "127.0.0.1", "name": "tabula-emu"},
    "connected": true, "synced": 3243, "total": 3243, "missing": [],
    "armed": false, "last_seen": "2026-10-17T12:36:52Z"
  }
}
```

`connecting` stays true until the initial sync is over (`synced` reaches `total`).

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
  token (e.g. `" -6.0"`, `"GATE"`, `"\"Vx Remy\""`); `value` the raw value
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

## Events

`GET /api/events` — Server-Sent Events:

- `status`: a `Status` (sent on connect and on every state change of the connection);
- `change`: `[{"addr", "text", "value", "origin"}]` with origin `console`
  (made on the console or by another remote app), `local` (made through
  this server from its machine) or `web <name>` (through an access link);
- `access`: the musicians' permission changed — read `/api/whoami` again;
- `resync`: the initial sync finished — reload `/api/state`;
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
  then their compressor gains).

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
| GET | `/api/access` | | `{"grants": [Grant], "musicians_can_write", "password", "current"?, "guest", "urls", "reachable"}` |
| POST | `/api/access` | `{"name": "Remy", "role": "musician", "bus": 3}` | 201 + `Grant` |
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

`login` and a wrong `current` password count as tries: after 5 wrong
passwords in a row a client waits 30 s, twice as long after each further
one (up to an hour), and all clients together get 30 wrong passwords a
minute at most. The password is kept as a PBKDF2-SHA256 hash in the access
file; `tabula -reset-password` removes it.

## Recording and replay

Sessions are directories under `-sessions` (default `./sessions`), named by
their start time (`2026-10-17_203000`): `meta.json`, `start.scn` (the
snapshot, a regular scene file), `start.nodes` (other nodes), `events.jsonl`
(one change or marker per line: `{"t": seconds, "addr", "text", "value",
"origin"}` or `{"t", "marker"}`).

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/record` | | `Record` |
| POST | `/api/record/start` | `{"note": "set 1"}` | `Record` (409 if not connected or already recording) |
| POST | `/api/record/mark` | `{"label": "Song 2"}` (default: elapsed time) | `Record` |
| POST | `/api/record/stop` | | `Record` |
| GET | `/api/sessions` | | `[{"name", "meta"}]`, newest first |
| GET | `/api/sessions/{name}` | | `{"name", "meta", "markers": [Event]}` |
| POST | `/api/replay/start` | `{"session": name, "from": seconds, "speed": 1}` | 202 + `Record`; 403 unless writes are armed (a replay writes to the console) |
| POST | `/api/replay/stop` | | `Record` |

`Record`: `{"recording", "name"?, "meta"?: {"start", "events", "markers", "duration", "audio"?, …}, "replaying", "replay_of"?, "replay_at"?, "error"?, "audio"?}`.
`audio` is the state of the audio capture of the recording (or of the last
one), an `AudioStatus` without `device` and `command` (viewers see the
record status); `meta.audio` the audio section of the session (see Audio).
A replay first sends the state at `from` (snapshot plus the changes up to
then), then each later change at its time divided by `speed`.

Events: `record` carries a `Record` (every second while recording, and on
every replay progress).

`tabula -record -console <ip>` starts recording as soon as the console is
synced and stops on exit — for unattended recording of a concert.

## Audio

When audio is enabled, starting a recording also records the tracks of the
console's USB card into the session's `audio/` directory, one WAV file per
track, and stopping the recording stops it (see
[recording](recording.md)). An audio capture that fails is reported
(`Record.audio.state` `failed`, `error`) and the recording of the changes
goes on. Engineer only.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/api/audio` | | `AudioView` |
| POST | `/api/audio/config` | `AudioConfig` | `AudioView`; 400 if not valid. Used from the next recording, saved in `<sessions>/audio.json` |
| GET | `/api/audio/devices` | | `[{"recorder", "id", "name", "channels"?, "format"?, "rates"?, "console"?}]`: what the recorders can record from (`id` is the device to use); `console` marks what looks like the console's card. Lists only, opens nothing |
| POST | `/api/audio/test` | `AudioConfig` (optional: default the saved one) | `AudioStatus` after 3 s of capture, nothing written: the stream format, `peaks`, or `error`; 409 while recording |
| GET | `/api/sessions/{name}/export?format=` | | a download: `reaper` (`<name>.rpp`), `audacity` (`<name> labels.txt`: `start⇥end⇥label` per marker, in seconds of the audio files), `csv` (`<name>.csv`: `time,audio_time,clock,type,address,text,value,origin,label`) |

`AudioConfig`: `{"enabled", "recorder": "arecord"|"parecord"|"ffmpeg"|"custom",
"device", "channels": 32, "rate": 48000, "tracks"?: "1-16", "bits": 24,
"plain_names"?, "command"?, "input"?}` — `command` is the custom
recorder's command line (`{device}`, `{channels}`, `{rate}` replaced),
writing raw samples in the `input` format (`s16le`, `s24le`, `s24-32le`,
`s32le`, `f32le`) or a WAV stream.

`AudioView`: `{"config", "command"? (what the configuration runs), "error"?
(what is wrong with it), "templates": {recorder: command}, "byte_rate",
"dir", "free", "remaining", "status"?: AudioStatus, "testing"}` — `free`
is the free space in the sessions directory (bytes, -1 unknown),
`remaining` how many seconds of audio it holds at `byte_rate`.

`AudioInfo` (the `audio` section of a session's `meta.json`): `{"rate",
"bits", "channels", "tracks": [{"channel", "name"?, "file"}], "offset",
"start", "drift_ppm"?, "frames", "device"?, "command"?, "overruns"?,
"error"?}`. `file` is relative to the session; `offset` is the time of the
first sample in seconds after the session start; a session time `t` is at
`(t − offset) / (1 + drift_ppm·10⁻⁶)` in the files.

`AudioStatus`: an `AudioInfo` plus `{"state": "starting"|"recording"|"stopped"|"failed",
"warnings"?, "format"?, "seconds", "bytes", "byte_rate", "free",
"remaining", "peaks"?}`: `remaining` is the audio the disk still holds
before the capture stops itself (seconds), `peaks` the peak level of each
track over the last second (linear, 1 = full scale). Warnings: the disk is
almost full (under 20 minutes), the recorder lost samples, no audio for a
while.
