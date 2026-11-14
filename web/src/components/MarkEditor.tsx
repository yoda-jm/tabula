// The mark editor: a session's markers renamed, moved, removed or added,
// saved at once (PUT /api/sessions/{name}/markers). In the Local replay
// band it also sets a marker to the moment playing and goes to one.

import { useEffect, useState } from "react";
import { formatMarkTime, parseTime, sessionApi } from "../lib/session";
import { store } from "../store/store";

interface Row {
  key: number;
  time: string;
  label: string;
  origin?: string;
}

let nextKey = 1;

export function MarkEditor(props: {
  session: string;
  duration: number;
  /** the replay's position: rows can be set to it, and a new one is added there */
  now?: () => number;
  onSeek?: (t: number) => void;
  onClose(): void;
}) {
  const { session, duration, now, onSeek, onClose } = props;
  const [rows, setRows] = useState<Row[] | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    sessionApi.session(session).then(
      (d) =>
        setRows(
          (d.markers ?? []).map((m) => ({ key: nextKey++, time: formatMarkTime(m.t), label: m.marker ?? "", origin: m.origin })),
        ),
      (e: Error) => {
        store.toast(`Markers: ${e.message}`, "error");
        onClose();
      },
    );
  }, [session]);
  if (!rows) return <p className="muted local-picker">Loading the markers…</p>;

  const set = (key: number, change: Partial<Row>) => setRows(rows.map((r) => (r.key === key ? { ...r, ...change } : r)));
  const timeOf = (r: Row) => parseTime(r.time);
  const bad = (r: Row) => {
    const t = timeOf(r);
    return Number.isNaN(t) || t < 0 || t > duration || !r.label.trim();
  };
  const invalid = rows.some(bad);
  const add = () => {
    const t = now ? now() : 0;
    setRows([...rows, { key: nextKey++, time: formatMarkTime(t), label: `Mark ${formatMarkTime(t).replace(/\.\d$/, "")}` }]);
  };
  const save = async () => {
    setBusy(true);
    try {
      await sessionApi.setMarkers(
        session,
        rows.map((r) => ({ label: r.label, t: timeOf(r), origin: r.origin })),
      );
      store.toast(`Markers saved: ${rows.length}`, "info");
      onClose();
    } catch (e) {
      store.toast(`Markers: ${(e as Error).message}`, "error");
    } finally {
      setBusy(false);
    }
  };
  const sorted = [...rows].sort((a, b) => (timeOf(a) || 0) - (timeOf(b) || 0));
  return (
    <div className="mark-editor" role="dialog" aria-label="Markers of the session">
      {sorted.length === 0 && <p className="muted">No marker.</p>}
      <div className="mark-rows">
        {sorted.map((r) => (
          <div key={r.key} className={"mark-row" + (bad(r) ? " invalid" : "")}>
            <input
              className="input num mark-time"
              value={r.time}
              aria-label="Time (m:ss.s)"
              aria-invalid={Number.isNaN(timeOf(r)) || timeOf(r) > duration}
              onChange={(e) => set(r.key, { time: e.target.value })}
            />
            {now && (
              <button type="button" className="btn small" title="Set to the moment playing" onClick={() => set(r.key, { time: formatMarkTime(now()) })}>
                Now
              </button>
            )}
            {onSeek && (
              <button
                type="button"
                className="btn small"
                title="Go there"
                disabled={Number.isNaN(timeOf(r))}
                onClick={() => onSeek(timeOf(r))}
              >
                Go
              </button>
            )}
            <input
              className="input mark-label"
              value={r.label}
              maxLength={80}
              aria-label="Label"
              aria-invalid={!r.label.trim()}
              onChange={(e) => set(r.key, { label: e.target.value })}
            />
            <button
              type="button"
              className="btn small"
              title="Remove this marker"
              aria-label={`Remove ${r.label}`}
              onClick={() => setRows(rows.filter((x) => x.key !== r.key))}
            >
              ✕
            </button>
          </div>
        ))}
      </div>
      <div className="row">
        <button type="button" className="btn small" onClick={add}>
          + Marker{now ? " here" : ""}
        </button>
        <button type="button" className="btn primary small" disabled={busy || invalid} onClick={() => void save()}>
          Save the markers
        </button>
        <button type="button" className="btn small" disabled={busy} onClick={onClose}>
          Cancel
        </button>
        {invalid && <span className="muted">A time from 0:00 to {formatMarkTime(duration)} (m:ss.s) and a label for each.</span>}
      </div>
    </div>
  );
}
