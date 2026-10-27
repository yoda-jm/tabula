// Recording and replay of shows: start/stop with a note, markers (M key),
// the recorded sessions, and replaying one onto the console. A session is
// the console (a snapshot of its settings, then every change) and, when the
// audio card is on, the sound of the console's USB card; the console can
// keep a stereo backup on its own USB stick (UsbBackup.tsx).

import { useCallback, useEffect, useState } from "react";
import { placeMarker } from "../components/Marker";
import { ConfirmDialog } from "../components/shell";
import { ApiError } from "../lib/api";
import type { AudioView } from "../lib/audio";
import type { Record as RecordStatus, SessionDetail, SessionSummary } from "../lib/session";
import { audioApi, audioPlan, audioSummary, exportUrl, formatBytes, plural } from "../lib/audio";
import { damagedText, formatDuration, parseTime, sessionApi } from "../lib/session";
import { nextSong, setlistApi, songTimes } from "../lib/setlist";
import { PLAY_BLOCKS, ROUTSWITCH, playRouting, playableTracks } from "../lib/soundcheck";
import { useCanWrite, useRecord, useSetlist, useStatus, useValue } from "../store/hooks";
import { store } from "../store/store";
import type { Audio } from "./AudioPanel";
import { AudioPanel, CheckProblems, SessionAudio, saveDraft, useAudio, useUnsaved } from "./AudioPanel";
import { SetlistCard } from "./Setlist";
import { CheckSummaryLine } from "./CheckPage";
import { TakeHome } from "./TakeHome";
import { UsbBackupCard } from "./UsbBackup";

const SPEEDS = [0.5, 1, 2, 3, 4, 8];

export function RecordPage() {
  const rec = useRecord();
  const [sessions, setSessions] = useState<SessionSummary[] | null>(null);
  const [listError, setListError] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const audio = useAudio(!!rec?.recording);
  // starts the audio of the recording again, with the saved settings
  const restartAudio = useCallback(async () => store.setRecord(await audioApi.restart()), []);
  // the settings' own button says it saves the page's changes first
  const saveAndRestart = useCallback(async () => {
    await saveDraft(audio);
    await restartAudio();
  }, [audio, restartAudio]);

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
      <Recorder rec={rec} audio={audio} onRetry={restartAudio} onRestart={() => void load()} />
      <SetlistCard />
      <AudioPanel rec={rec} audio={audio} onRetry={saveAndRestart} />
      <UsbBackupCard rec={rec} />
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
                <span className="muted">{plural(s.meta.events, "change")}</span>
                <span className="muted">{plural(s.meta.markers, "marker")}</span>
                <span className={s.meta.audio?.error ? "danger-text" : "muted"}>{audioSummary(s.meta.audio)}</span>
                <span className="session-note">{s.meta.note || s.meta.console || ""}</span>
              </button>
            );
          })}
        </div>
      </section>
      {selected && (
        <Session
          key={selected}
          name={selected}
          rec={rec}
          audio={audio.view}
          onDeleted={() => {
            store.toast(`Session ${selected} deleted`);
            setSelected(null);
            void load();
          }}
        />
      )}
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

/** RecordsWhat lists what a session keeps: the console always, the audio when its card is on, and the backup on the stick. */
function RecordsWhat({ audio, live, usb }: { audio: string; live?: boolean; usb?: boolean }) {
  return (
    <ul className="records-what">
      <li>
        <span className="rec-kind">Console</span>
        <span>
          a snapshot of every setting at the start (a scene file that opens in M32-Edit), then every change, on the console or here
          (faders, mutes, EQ, routing, scene recalls…) with its time, and your markers. Settings only, not the sound; replaying a
          session moves the console the same way.
        </span>
      </li>
      <li className={audio ? "" : "off"}>
        <span className="rec-kind">Audio</span>
        <span>
          {audio
            ? `the sound of the console's USB card: ${audio}, in step with the console changes.`
            : live
              ? "not recorded in this session."
              : "not recorded: turn on Audio tracks below to record the sound too."}
        </span>
      </li>
      <li className={usb ? "" : "off"}>
        <span className="rec-kind">USB stick</span>
        <span>
          {usb
            ? "a stereo backup made by the console on its own USB stick, from what feeds its recorder (the main mix by default)."
            : "no backup on the console's stick: turn it on below."}
        </span>
      </li>
    </ul>
  );
}

/** AudioAlarm says, where the recording is watched, that its audio is not recorded, and how to get it back. */
function AudioAlarm(props: {
  rec: RecordStatus;
  /** asks what to do with the audio settings not saved (useUnsaved) */
  ask(verb: string, go: () => Promise<unknown>): void;
  onRetry(): Promise<void>;
  onRestart(): Promise<void>;
}) {
  const { rec, ask, onRetry, onRestart } = props;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const a = rec.audio;
  if (!a || a.state !== "failed") return null;
  const act = async (f: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await f();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="rec-alarm" role="alert">
      <p>
        <strong>⚠ The audio is NOT recorded.</strong> The console still is.
      </p>
      <p className="rec-alarm-error">{a.error || "the audio stopped"}</p>
      {a.warnings?.map((w) => (
        <p key={w} className="rec-alarm-error">
          ⚠ {w}
        </p>
      ))}
      {a.frames ? (
        <div className="row">
          <span className="grow">
            {formatDuration(a.frames / a.rate)} of audio were recorded before it stopped: they stay in this session.
          </span>
          <button type="button" className="btn primary" disabled={busy} onClick={() => ask("record again", () => act(onRestart))}>
            {busy ? "Restarting…" : "Stop and record again"}
          </button>
        </div>
      ) : (
        <div className="row">
          <span className="grow">Nothing was recorded yet: fix the device under Audio tracks, then start the audio of this recording.</span>
          <button type="button" className="btn primary" disabled={busy} onClick={() => ask("start the audio", () => act(onRetry))}>
            {busy ? "Starting…" : "Start the audio"}
          </button>
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </div>
  );
}

function Recorder(props: { rec: RecordStatus | null; audio: Audio; onRetry(): Promise<void>; onRestart(): void }) {
  const { rec, audio, onRetry, onRestart } = props;
  const status = useStatus();
  const [note, setNote] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [stopping, setStopping] = useState(false); // asking before the show stops being recorded
  const recording = !!rec?.recording;
  const nextLabel = nextSong(useSetlist());
  const unsaved = useUnsaved(audio);
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
  // a label typed, or the next song of the setlist (the M key, on every page, is the top bar's)
  const mark = async (text: string) => {
    if (await placeMarker(text.trim() ? () => setlistApi.markLabel(text.trim()) : setlistApi.markNext)) setLabel("");
  };

  if (!rec) return null;
  const err = error || rec.error;
  return (
    <section className="card">
      <h2>Recording a session</h2>
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
                {rec.no_console ? (
                  <span className="warn-text" title="Its moves join the session once it is connected and synced">
                    console: not connected yet
                  </span>
                ) : (
                  <span>console: {plural(rec.meta?.events ?? 0, "change")}</span>
                )}
                <span>{plural(rec.meta?.markers ?? 0, "marker")}</span>
                {rec.audio?.state === "failed" ? (
                  <strong className="danger-text">⚠ NO AUDIO</strong>
                ) : (
                  <span>{rec.audio ? `audio: ${plural(rec.audio.tracks?.length ?? 0, "track")}` : "no audio"}</span>
                )}
                {rec.usb?.on && !rec.usb.starting && (
                  rec.usb.error ? <strong className="danger-text">⚠ NO USB BACKUP</strong> : <span>USB stick: backup</span>
                )}
              </div>
              {rec.meta?.note && <p className="muted">{rec.meta.note}</p>}
              <p className="muted mono">{rec.name}</p>
            </div>
          </div>
          <AudioAlarm
            rec={rec}
            ask={unsaved.ask}
            onRetry={onRetry}
            onRestart={async () => {
              // errors show in this card: the alarm goes with the recording
              const note = rec.meta?.note ?? "";
              const ok = await run(async () => {
                store.setRecord(await sessionApi.stop());
                return sessionApi.start(note);
              });
              if (ok) onRestart();
            }}
          />
          {rec.audio?.state !== "failed" &&
            rec.audio?.warnings?.map((w) => (
              <p key={w} className="warn-box" role="status">
                ⚠ Audio: {w}
              </p>
            ))}
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              void mark(label);
            }}
          >
            <label className="grow">
              <span className="field-label">Marker label (empty: the next song of the setlist; M on any page marks it too)</span>
              <input className="input" type="text" value={label} placeholder={nextLabel ?? "Song 2"} onChange={(e) => setLabel(e.target.value)} />
            </label>
            <button type="submit" className="btn big">
              Marker
            </button>
            <button type="button" className="btn big danger" disabled={busy} onClick={() => setStopping(true)}>
              Stop recording
            </button>
          </form>
          {stopping && (
            <ConfirmDialog
              title="Stop the recording?"
              confirm="Stop recording"
              onCancel={() => setStopping(false)}
              onConfirm={() => {
                setStopping(false);
                void run(sessionApi.stop);
              }}
            >
              <p>
                <strong>The session and its audio end there</strong>: what follows is not recorded, unless a new recording is
                started.
              </p>
            </ConfirmDialog>
          )}
        </>
      ) : (
        <>
          <RecordsWhat audio={audioPlan(audio.draft ?? audio.view?.config)} usb={rec.usb?.on} />
          <CheckProblems audio={audio} />
          <CheckSummaryLine />
          {audio.draft && <p className="note">The audio settings below are not saved yet: Start recording asks whether to save them.</p>}
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              // a recording uses the saved settings: save the page's, or not
              unsaved.ask("start", () => run(() => sessionApi.start(note.trim())).then((ok) => ok && setNote("")));
            }}
          >
            <label className="grow">
              <span className="field-label">Note (optional)</span>
              <input className="input" type="text" value={note} placeholder="Set 1, venue…" onChange={(e) => setNote(e.target.value)} />
            </label>
            <button type="submit" className="btn big rec-start" disabled={busy || (!status.connected && !rec.audio_on)}>
              ● Start recording
            </button>
          </form>
        </>
      )}
      {!recording && !status.connected && (
        <p className="muted">
          {rec.audio_on
            ? "No console connected: Start records the audio alone, and the console's moves join the session once it is connected and synced (a Wi-Fi down at the start does not cost the recording)."
            : "Connect to a console to record it (with the audio tracks on, the audio can be recorded without it)."}
        </p>
      )}
      {err && (
        <p className="error" role="alert">
          {err}
        </p>
      )}
      {recording && rec.waiting ? (
        <p className="warn-box" role="status">
          The disk is slow: {plural(rec.waiting, "console change")} waiting to be written. They are kept and written once it
          takes them; the live view is not held up.
        </p>
      ) : null}
      {unsaved.dialog}
    </section>
  );
}

function Session(props: { name: string; rec: RecordStatus | null; audio: AudioView | null; onDeleted(): void }) {
  const { name, rec, onDeleted } = props;
  const can = useCanWrite();
  const [detail, setDetail] = useState<SessionDetail | null>(null);
  const [error, setError] = useState("");
  const [from, setFrom] = useState("0");
  const [speedChosen, setSpeed] = useState(1);
  // a virtual soundcheck: the tracks play to the card with the replay, at 1×
  const [withTracks, setWithTracks] = useState(false);
  const [toCard, setToCard] = useState(true);
  const speed = withTracks ? 1 : speedChosen;
  const [asking, setAsking] = useState(false);
  const [deleting, setDeleting] = useState(false);
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
      store.setRecord(await sessionApi.replay(name, fromSeconds, speed, withTracks ? { audio: true, routing: toCard } : undefined));
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
  const remove = async () => {
    setDeleting(false);
    setError("");
    try {
      await sessionApi.remove(name);
      onDeleted();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const audio = detail?.meta.audio;
  const audioBytes = audio ? audio.frames * (audio.tracks?.length ?? 0) * (audio.bits / 8) : 0;
  const tracks = playableTracks(audio);
  const soundcheck = withTracks && tracks > 0;

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
          <dt>At the start</dt>
          <dd>
            {detail.snapshot ? (
              <>
                every setting of the console ({detail.snapshot} lines)
                {detail.scene && (
                  <>
                    {" "}
                    <a className="btn" href={exportUrl(name, "scene")} download title="The console at the start: a scene file that opens in M32-Edit">
                      ⤓ Scene (.scn)
                    </a>
                  </>
                )}
              </>
            ) : (
              <span className="danger-text">not saved</span>
            )}
          </dd>
          <dt>Console</dt>
          <dd>
            {!detail.meta.console && !detail.meta.events
              ? "not connected during the recording: the audio alone"
              : `${plural(detail.meta.events, "change")} after the start`}
            ; {plural(detail.meta.markers, "marker")}
            {detail.meta.console_from ? ` (the console joined at ${formatDuration(detail.meta.console_from)})` : ""}
          </dd>
          {detail.meta.console && (
            <>
              <dt>Recorded from</dt>
              <dd>{detail.meta.console}</dd>
            </>
          )}
          {detail.meta.note && (
            <>
              <dt>Note</dt>
              <dd>{detail.meta.note}</dd>
            </>
          )}
          {detail.meta.usb_backup && (
            <>
              <dt>USB stick</dt>
              {detail.meta.usb_backup.error ? (
                <dd className="danger-text">no backup: {detail.meta.usb_backup.error}</dd>
              ) : (
                <dd>
                  backup in <code>{detail.meta.usb_backup.file}</code> on the console's stick
                  {detail.meta.usb_backup.already && " (a recording started on the console)"}
                </dd>
              )}
            </>
          )}
        </dl>
      )}
      {detail?.damaged?.length ? (
        <p className="warn-box" role="status">
          ⚠ {damagedText(detail.damaged)}
        </p>
      ) : null}

      {detail?.setlist && detail.setlist.length > 0 && (
        <>
          <h4>Setlist</h4>
          <ol className="setlist-times">
            {detail.setlist.map((song, i) => {
              const t = songTimes(detail.setlist!, detail.markers)[i];
              return (
                <li key={i}>
                  <span className="marker-label">{song}</span>
                  {t === undefined ? (
                    <span className="muted">not marked</span>
                  ) : (
                    <>
                      <code>{formatDuration(t)}</code>
                      <button type="button" className="btn" onClick={() => setFrom(String(Math.floor(t)))}>
                        Replay from here
                      </button>
                    </>
                  )}
                </li>
              );
            })}
          </ol>
        </>
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
      {detail && <TakeHome detail={detail} recording={!!rec?.recording} recordingThis={recordingThis} />}

      <h4>Replay onto the console</h4>
      <p className="muted">
        A replay moves the console as it was recorded: its settings, faders and mutes.
        {tracks > 0
          ? " With the virtual soundcheck, the recorded tracks play to the console's card too, in step: the show can be mixed again without the band."
          : " It plays no sound; open the audio in Reaper or Audacity with the exports above."}
      </p>
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
          <select
            className="select"
            value={speed}
            disabled={soundcheck}
            title={soundcheck ? "A virtual soundcheck plays at 1×: the tracks are not sped up" : undefined}
            onChange={(e) => setSpeed(Number(e.target.value))}
          >
            {SPEEDS.map((s) => (
              <option key={s} value={s}>
                {s}×
              </option>
            ))}
          </select>
        </label>
        {replayingThis ? (
          <button type="button" className="btn big danger" onClick={() => void stop()}>
            {rec?.replay_audio ? "■ Stop the soundcheck" : "■ Stop replay"}
          </button>
        ) : (
          <button
            type="button"
            className="btn big primary"
            disabled={!can || Number.isNaN(fromSeconds) || replayingOther || recordingThis || (soundcheck && !!rec?.recording)}
            onClick={() => setAsking(true)}
          >
            {soundcheck ? "▶ Start the soundcheck" : "▶ Replay"}
          </button>
        )}
      </div>
      {tracks > 0 && (
        <div className="soundcheck">
          <label className="check">
            <input type="checkbox" checked={withTracks} disabled={replayingThis} onChange={(e) => setWithTracks(e.target.checked)} />
            Virtual soundcheck: play the {plural(tracks, "recorded track")} to the console's card too (at 1×)
          </label>
          {soundcheck && (
            <>
              <label className="check">
                <input type="checkbox" checked={toCard} disabled={replayingThis} onChange={(e) => setToCard(e.target.checked)} />
                Switch the console's inputs to the card for the time of it (routing PLAY), and back as they were at the end
              </label>
              <PlayRoutingNote toCard={toCard} />
              {props.audio?.play_error ? (
                <p className="error">The tracks cannot be played: {props.audio.play_error} (Audio settings above).</p>
              ) : (
                props.audio?.play_command && (
                  <p className="muted">
                    Plays with <code className="wrap">{props.audio.play_command}</code>
                  </p>
                )
              )}
              {rec?.recording && <p className="warn-box">A recording is running: stop it before a virtual soundcheck.</p>}
            </>
          )}
        </div>
      )}
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
      <div className="row session-delete">
        <button
          type="button"
          className="btn danger-text"
          disabled={recordingThis || replayingThis}
          title={recordingThis || replayingThis ? "Not while it is recorded or replayed" : undefined}
          onClick={() => setDeleting(true)}
        >
          Delete this session…
        </button>
      </div>
      {deleting && (
        <ConfirmDialog title="Delete this session?" confirm="Delete" onConfirm={() => void remove()} onCancel={() => setDeleting(false)}>
          <p>
            <strong>This removes the folder {name}</strong>: the console at the start, {plural(detail?.meta.events ?? 0, "change")},{" "}
            {plural(detail?.meta.markers ?? 0, "marker")}
            {audio ? `, and ${plural(audio.tracks?.length ?? 0, "audio file")} (${formatBytes(audioBytes)})` : ""}.
          </p>
          <p className="muted">It cannot be undone: take it home first (above) to keep it.</p>
        </ConfirmDialog>
      )}
      {asking && !soundcheck && (
        <ConfirmDialog title="Replay this session?" confirm="Replay" onConfirm={() => void start()} onCancel={() => setAsking(false)}>
          <p>
            <strong>Replay writes the recorded settings to the console.</strong> It first sets the state at{" "}
            {formatDuration(fromSeconds)}, then plays every recorded change{speed !== 1 ? ` at ${speed}× speed` : ""}.
          </p>
          <p className="muted">Faders and settings on the live console will move.</p>
        </ConfirmDialog>
      )}
      {asking && soundcheck && (
        <ConfirmDialog
          title="Start a virtual soundcheck?"
          confirm="Start the soundcheck"
          onConfirm={() => void start()}
          onCancel={() => setAsking(false)}
        >
          <p>
            <strong>The {plural(tracks, "recorded track")} play to the console's card</strong> from {formatDuration(fromSeconds)},
            and the console moves as recorded: its settings, faders and mutes change at once, then with every recorded change.
          </p>
          <p>
            {toCard
              ? "The console's inputs switch to the card (routing PLAY): the stage is not heard until the end or Stop, when they go back as they were."
              : "The routing is left as it is: the console's inputs hear the card only if they are switched to it (Routing page, PLAY)."}
          </p>
          <p className="muted">tabula first saves the console's settings as a backup (Scenes → Scene files).</p>
        </ConfirmDialog>
      )}
    </section>
  );
}

/** PlayRoutingNote says whether the console's inputs will hear the card during a virtual soundcheck. */
export function PlayRoutingNote({ toCard }: { toCard: boolean }) {
  // a fixed list: the hooks run in the same order every time
  const values = [ROUTSWITCH, ...PLAY_BLOCKS.map((b) => `/config/routing/PLAY/${b}`)];
  const texts = new Map(values.map((a) => [a, useValue(a)?.text]));
  const r = playRouting((a) => texts.get(a));
  if (!toCard && !r.playing) {
    return <p className="warn-box">The console's inputs are on the stage (routing REC): they will not hear the tracks.</p>;
  }
  if (!r.card.length) {
    return (
      <p className="warn-box">
        The PLAY routing takes no input from the card: set it on the Routing page (inputs 1-8 ← CARD1-8…), or the tracks are not heard.
      </p>
    );
  }
  return (
    <p className="muted">
      {r.playing ? "The inputs already hear the PLAY routing" : "The PLAY routing"} takes the card on inputs {r.card.join(", ")}.
    </p>
  );
}
