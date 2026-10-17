// Recording and replay of shows: start/stop with a note, markers (M key),
// the recorded sessions, and replaying one onto the console.

import { useCallback, useEffect, useRef, useState } from "react";
import { ConfirmDialog } from "../components/shell";
import { ApiError } from "../lib/api";
import type { Record as RecordStatus, SessionDetail, SessionSummary } from "../lib/session";
import { formatDuration, parseTime, sessionApi } from "../lib/session";
import { useCanWrite, useRecord, useStatus } from "../store/hooks";
import { store } from "../store/store";
import { AudioPanel, SessionAudio } from "./AudioPanel";

const SPEEDS = [0.5, 1, 2, 4, 8];

export function RecordPage() {
  const rec = useRecord();
  const [sessions, setSessions] = useState<SessionSummary[] | null>(null);
  const [listError, setListError] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setSessions(await sessionApi.sessions());
      setListError("");
    } catch (e) {
      setListError((e as Error).message);
    }
  }, []);

  // reload the list when a recording starts or stops
  const recording = !!rec?.recording;
  useEffect(() => {
    void load();
  }, [load, recording, rec?.name]);

  if (rec === null && sessions === null && listError) {
    return (
      <div className="page record">
        <p className="banner">This tabula has no recording support ({listError}).</p>
      </div>
    );
  }
  return (
    <div className="page record">
      <Recorder rec={rec} />
      <AudioPanel rec={rec} />
      <section className="card">
        <div className="section-head">
          <h2>Sessions</h2>
          <button type="button" className="btn" onClick={() => void load()}>
            Refresh
          </button>
        </div>
        {listError && <p className="error">{listError}</p>}
        {sessions && sessions.length === 0 && <p className="muted">No session recorded yet.</p>}
        <div className="sessions">
          {sessions?.map((listed) => {
            // the session being recorded: its live description
            const s = rec?.recording && rec.name === listed.name && rec.meta ? { ...listed, meta: rec.meta } : listed;
            return (
              <button
                key={s.name}
                type="button"
                className={"btn session-item" + (s.name === selected ? " active" : "")}
                aria-pressed={s.name === selected}
                onClick={() => setSelected(s.name === selected ? null : s.name)}
              >
                <span>
                  <strong>{formatStart(s.meta.start)}</strong>
                  {rec?.recording && rec.name === s.name && <span className="danger-text"> · recording</span>}
                  {rec?.replaying && rec.replay_of === s.name && <span className="note"> · replaying</span>}
                </span>
                <span className="mono">{formatDuration(s.meta.duration)}</span>
                <span className="muted">{s.meta.events} events</span>
                <span className="muted">{s.meta.markers} markers</span>
                <span className="session-note">{s.meta.note || s.meta.console || ""}</span>
              </button>
            );
          })}
        </div>
      </section>
      {selected && <Session key={selected} name={selected} rec={rec} />}
    </div>
  );
}

function formatStart(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** useTick re-renders every `ms` while `on`. */
function useTick(on: boolean, ms: number) {
  const [, set] = useState(0);
  useEffect(() => {
    if (!on) return;
    const t = setInterval(() => set((n) => n + 1), ms);
    return () => clearInterval(t);
  }, [on, ms]);
}

function Recorder({ rec }: { rec: RecordStatus | null }) {
  const status = useStatus();
  const [note, setNote] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const recording = !!rec?.recording;
  useTick(recording, 250);

  const run = async (f: () => Promise<RecordStatus>) => {
    setBusy(true);
    setError("");
    try {
      store.setRecord(await f());
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  };
  const mark = useCallback(
    async (text: string) => {
      try {
        store.setRecord(await sessionApi.mark(text.trim()));
        store.toast(text.trim() ? `Marker “${text.trim()}”` : "Marker added");
        setLabel("");
      } catch (e) {
        setError((e as Error).message);
      }
    },
    [],
  );

  // M adds a marker while recording (unless typing in a field)
  const labelRef = useRef(label);
  labelRef.current = label;
  useEffect(() => {
    if (!recording) return;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key.toLowerCase() !== "m" || e.ctrlKey || e.metaKey || e.altKey) return;
      if (t.closest("input, textarea, select, [contenteditable]")) return;
      e.preventDefault();
      void mark(labelRef.current);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [recording, mark]);

  if (!rec) return null;
  const err = error || rec.error;
  return (
    <section className="card">
      <h2>Recording</h2>
      {recording ? (
        <>
          <div className="rec-panel">
            <div className="elapsed live" aria-live="off">
              {formatDuration(store.recordElapsed())}
            </div>
            <div>
              <div className="rec-stats">
                <span className="danger-text">
                  <strong>● REC</strong>
                </span>
                <span>{rec.meta?.events ?? 0} events</span>
                <span>{rec.meta?.markers ?? 0} markers</span>
              </div>
              {rec.meta?.note && <p className="muted">{rec.meta.note}</p>}
              <p className="muted mono">{rec.name}</p>
            </div>
          </div>
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              void mark(label);
            }}
          >
            <label className="grow">
              <span className="field-label">Marker label (optional; press M anywhere on this page)</span>
              <input className="input" type="text" value={label} placeholder="Song 2" onChange={(e) => setLabel(e.target.value)} />
            </label>
            <button type="submit" className="btn big">
              Marker
            </button>
            <button type="button" className="btn big danger" disabled={busy} onClick={() => void run(sessionApi.stop)}>
              Stop recording
            </button>
          </form>
        </>
      ) : (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault();
            void run(() => sessionApi.start(note.trim())).then((ok) => ok && setNote(""));
          }}
        >
          <label className="grow">
            <span className="field-label">Note (optional)</span>
            <input className="input" type="text" value={note} placeholder="Set 1, venue…" onChange={(e) => setNote(e.target.value)} />
          </label>
          <button type="submit" className="btn big rec-start" disabled={busy || !status.connected}>
            ● Start recording
          </button>
        </form>
      )}
      {!recording && !status.connected && <p className="muted">Connect to a console to record it.</p>}
      {err && (
        <p className="error" role="alert">
          {err}
        </p>
      )}
    </section>
  );
}

function Session({ name, rec }: { name: string; rec: RecordStatus | null }) {
  const can = useCanWrite();
  const [detail, setDetail] = useState<SessionDetail | null>(null);
  const [error, setError] = useState("");
  const [from, setFrom] = useState("0");
  const [speed, setSpeed] = useState(1);
  const [asking, setAsking] = useState(false);
  const recordingThis = !!rec?.recording && rec.name === name;
  const markers = rec?.meta?.markers;

  useEffect(() => {
    sessionApi.session(name).then(setDetail, (e: Error) => setError(e.message));
  }, [name, recordingThis, recordingThis ? markers : 0]);

  const replayingThis = !!rec?.replaying && rec.replay_of === name;
  const replayingOther = !!rec?.replaying && rec.replay_of !== name;
  const fromSeconds = parseTime(from);
  const duration = detail?.meta.duration ?? 0;

  const start = async () => {
    setAsking(false);
    setError("");
    try {
      store.setRecord(await sessionApi.replay(name, fromSeconds, speed));
    } catch (e) {
      const msg = e instanceof ApiError && e.status === 403 ? `Refused: ${e.message}. Arm writes first.` : (e as Error).message;
      setError(msg);
    }
  };
  const stop = async () => {
    try {
      store.setRecord(await sessionApi.stopReplay());
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <section className="card">
      <h2>Session {name}</h2>
      {!detail && !error && <p className="muted">Loading…</p>}
      {detail && (
        <dl className="facts">
          <dt>Start</dt>
          <dd>{formatStart(detail.meta.start)}</dd>
          <dt>Duration</dt>
          <dd className="mono">{formatDuration(detail.meta.duration)}</dd>
          <dt>Events</dt>
          <dd>
            {detail.meta.events} changes, {detail.meta.markers} markers
          </dd>
          {detail.meta.console && (
            <>
              <dt>Console</dt>
              <dd>{detail.meta.console}</dd>
            </>
          )}
          {detail.meta.note && (
            <>
              <dt>Note</dt>
              <dd>{detail.meta.note}</dd>
            </>
          )}
        </dl>
      )}

      {detail?.markers && detail.markers.length > 0 && (
        <>
          <h4>Markers</h4>
          <ul className="markers">
            {detail.markers.map((m, i) => (
              <li key={i}>
                <code>{formatDuration(m.t)}</code>
                <span className="marker-label">{m.marker}</span>
                <button type="button" className="btn" onClick={() => setFrom(String(Math.floor(m.t)))}>
                  Replay from here
                </button>
              </li>
            ))}
          </ul>
        </>
      )}

      {detail && <SessionAudio detail={detail} />}

      <h4>Replay</h4>
      {!can && (
        <p className="warn-box">
          A replay writes the recorded mix to the console: it is refused until writes are armed (top right).
        </p>
      )}
      {recordingThis && <p className="warn-box">This session is still being recorded.</p>}
      {replayingOther && <p className="warn-box">Another session ({rec?.replay_of}) is replaying.</p>}
      <div className="row">
        <label>
          <span className="field-label">From (seconds or m:ss)</span>
          <input
            className="input num"
            type="text"
            inputMode="numeric"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            aria-invalid={Number.isNaN(fromSeconds)}
          />
        </label>
        {detail?.markers && detail.markers.length > 0 && (
          <label>
            <span className="field-label">or a marker</span>
            <select className="select" value="" onChange={(e) => e.target.value && setFrom(e.target.value)}>
              <option value="">Choose…</option>
              {detail.markers.map((m, i) => (
                <option key={i} value={String(Math.floor(m.t))}>
                  {formatDuration(m.t)} {m.marker}
                </option>
              ))}
            </select>
          </label>
        )}
        <label>
          <span className="field-label">Speed</span>
          <select className="select" value={speed} onChange={(e) => setSpeed(Number(e.target.value))}>
            {SPEEDS.map((s) => (
              <option key={s} value={s}>
                {s}×
              </option>
            ))}
          </select>
        </label>
        {replayingThis ? (
          <button type="button" className="btn big danger" onClick={() => void stop()}>
            ■ Stop replay
          </button>
        ) : (
          <button
            type="button"
            className="btn big primary"
            disabled={!can || Number.isNaN(fromSeconds) || replayingOther || recordingThis}
            onClick={() => setAsking(true)}
          >
            ▶ Replay
          </button>
        )}
      </div>
      {replayingThis && (
        <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={duration} aria-valuenow={rec?.replay_at ?? 0}>
          <div className="progress-bar" style={{ width: `${duration ? (100 * (rec?.replay_at ?? 0)) / duration : 0}%` }} />
          <span className="progress-text">
            {formatDuration(rec?.replay_at ?? 0)} / {formatDuration(duration)}
          </span>
        </div>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {asking && (
        <ConfirmDialog title="Replay this session?" confirm="Replay" onConfirm={() => void start()} onCancel={() => setAsking(false)}>
          <p>
            <strong>Replay writes the recorded mix to the console.</strong> It first sets the state at{" "}
            {formatDuration(fromSeconds)}, then plays every recorded change{speed !== 1 ? ` at ${speed}× speed` : ""}.
          </p>
          <p className="muted">Faders and settings on the live console will move.</p>
        </ConfirmDialog>
      )}
    </section>
  );
}
