# tabula client HTTP API

`tabula` serves its GUI and this API on one HTTP port (default
`localhost:8080`). It holds at most one console connection, shared by every
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
index or name). 204 on success, 403 if writes are not armed, 400 otherwise.
The value is quantized like the console does; the resulting change comes
back on the event stream.

## Events

`GET /api/events` — Server-Sent Events:

- `status`: a `Status` (sent on connect and on every state change of the connection);
- `change`: `[{"addr", "text", "value", "origin"}]` with origin `console`
  (made on the console or by another remote app) or `local` (made through
  this server);
- `resync`: the initial sync finished — reload `/api/state`.
