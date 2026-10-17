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

Every API request is checked against the caller's role (see
[access](access.md)): requests from the machine running `tabula` are the
engineer's; other devices use an access link, which sets a cookie
(`tabula_access`), or get the guest role (`-guest`, none by default).

| Least role | Routes |
|---|---|
| none | `GET /api/whoami`, `POST /api/logout`, `GET /a/{token}` |
| viewer | `GET /api/status`, `/api/schema`, `/api/state`, `/api/events`, `/api/record` |
| musician | `POST /api/set`, only for the sends to their bus (and its linked partner) from channels, aux inputs and FX returns (`on`, `level`, `pan`), and their bus `fader` and `on`; allowed without arming when the musicians may change their mix |
| engineer | everything else, including any route not listed here |

A refused request gets 403 with `{"error": "..."}`.

| Method | Path | Body | Answer |
|---|---|---|---|
| GET | `/a/{token}` | | sets the cookie and redirects to `/#/mix` (musician) or `/#/mixer`; 403 page if the link is not valid |
| GET | `/api/whoami` | | `{"role", "name"?, "bus"?, "buses"?, "local", "link", "musicians_can_write"}` |
| POST | `/api/logout` | | forgets the cookie; answers the new `whoami` |
| GET | `/api/access` | | `{"grants": [Grant], "musicians_can_write", "guest", "urls", "reachable"}` |
| POST | `/api/access` | `{"name": "Remy", "role": "musician", "bus": 3}` | 201 + `Grant` |
| DELETE | `/api/access/{id}` | | 204; event streams opened with it end |
| POST | `/api/access/settings` | `{"musicians_can_write": false}` | the access status |

`Grant`: `{"id", "name", "role", "bus"?, "token", "created"}`; its link is
one of `urls` + `/a/` + `token`. `urls` lists where other devices reach
this `tabula` (one per address of the machine when it listens on all of
them); `reachable` is false when it listens on the machine only.

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

`Record`: `{"recording", "name"?, "meta"?: {"start", "events", "markers", "duration", …}, "replaying", "replay_of"?, "replay_at"?, "error"?}`.
A replay first sends the state at `from` (snapshot plus the changes up to
then), then each later change at its time divided by `speed`.

Events: `record` carries a `Record` (every second while recording, and on
every replay progress).

`tabula -record -console <ip>` starts recording as soon as the console is
synced and stops on exit — for unattended recording of a concert.
