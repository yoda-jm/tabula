// Audio tracks recorded with the sessions: the settings (device, channels,
// tracks, format), a test of the input, the live state of the capture
// while recording, and the exports of a session for audio editors.

import { useCallback, useEffect, useState } from "react";
import type { AudioConfig, AudioDevice, AudioStatus, AudioTrack, AudioView, ExportFormat, Recorder } from "../lib/audio";
import { LOW_DISK_SECONDS, audioApi, byteRate, exportUrl, formatBytes, formatHours, formatTracks, parseTracks, trackLabel } from "../lib/audio";
import { meterFraction, toDbfs } from "../lib/meters";
import type { Record as RecordStatus, SessionDetail } from "../lib/session";
import { formatDuration } from "../lib/session";

const RECORDERS: { id: Recorder; label: string }[] = [
  { id: "arecord", label: "arecord (ALSA)" },
  { id: "parecord", label: "parecord (PulseAudio, PipeWire)" },
  { id: "ffmpeg", label: "ffmpeg" },
  { id: "custom", label: "Custom command" },
];
const BITS = [16, 24, 32];
const RATES = [44100, 48000, 96000];
const INPUTS = ["s32le", "s24-32le", "s24le", "s16le", "f32le"];

export interface Audio {
  view: AudioView | null;
  error: string;
  setView(v: AudioView): void;
  /** the settings changed on the page and not saved yet, or null */
  draft: AudioConfig | null;
  setDraft(c: AudioConfig | null): void;
}

/** useAudio loads the audio settings, again when a recording starts or stops (free space, last capture). */
export function useAudio(recording: boolean): Audio {
  const [view, setView] = useState<AudioView | null>(null);
  const [draft, setDraft] = useState<AudioConfig | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try {
      setView(await audioApi.get());
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load, recording]);
  return { view, error, setView, draft, setDraft };
}

/**
 * saveDraft saves the settings changed on the page, if any: a recording
 * uses the saved ones, so starting one (or its audio) saves them first.
 */
export async function saveDraft(audio: Audio): Promise<void> {
  if (!audio.draft) return;
  const v = await audioApi.save(audio.draft);
  audio.setView(v);
  audio.setDraft(null);
}

/** toConsoleCard switches the settings to the console's card, when the check found it connected, and saves them. */
export async function toConsoleCard(audio: Audio): Promise<void> {
  const card = audio.view?.check?.console;
  const base = audio.draft ?? audio.view?.config;
  if (!card || !base) return;
  const v = await audioApi.save({ ...base, recorder: card.recorder, device: card.id, ...(card.channels ? { channels: card.channels } : {}) });
  audio.setView(v);
  audio.setDraft(null);
}

/**
 * AudioPanel is the Record page's audio card. While recording, it shows the
 * capture; when the audio failed before recording anything (or is off), the
 * settings too, to fix them and start the audio of the recording (onRetry).
 */
export function AudioPanel({ rec, audio, onRetry }: { rec: RecordStatus | null; audio: Audio; onRetry(): Promise<void> }) {
  const { view, error, setView } = audio;
  const recording = !!rec?.recording;

  if (!view) {
    return error ? (
      <section className="card">
        <h2>Audio tracks</h2>
        <p className="error">{error}</p>
      </section>
    ) : null;
  }
  const live = recording ? rec?.audio : undefined;
  // the audio of this recording can (re)start: it failed before recording anything, or was off
  const fixable = recording && (!live || (live.state === "failed" && !live.frames));
  const settings = (retry?: () => Promise<void>) => (
    <AudioSettings key={JSON.stringify(view.config)} view={view} audio={audio} onSaved={setView} onRetry={retry} />
  );
  return (
    <section className="card audio">
      <div className="section-head">
        <h2>Audio tracks</h2>
        {!recording && <span className="muted">{view.config.enabled ? "recorded with every session" : "not recorded"}</span>}
      </div>
      {recording ? (
        <>
          {live ? (
            <AudioLive st={live} />
          ) : (
            <p className="muted">This recording has no audio: turn it on below, then Start the audio.</p>
          )}
          {fixable && (
            <>
              {live && <p className="note">Fix the settings below (Find devices: the console's card is marked ★), then Start the audio.</p>}
              {settings(onRetry)}
            </>
          )}
        </>
      ) : (
        <>
          {rec?.audio && <LastCapture st={rec.audio} />}
          {settings()}
        </>
      )}
    </section>
  );
}

/** AudioLive is the capture while recording: state, sizes, disk, levels. */
function AudioLive({ st }: { st: AudioStatus }) {
  const low = st.remaining >= 0 && st.remaining < LOW_DISK_SECONDS;
  return (
    <>
      <div className="rec-stats audio-stats">
        <AudioState st={st} />
        <span className="mono">{formatDuration(st.seconds)}</span>
        <span>{st.tracks?.length ?? 0} tracks</span>
        <span>{formatBytes(st.bytes)}</span>
        <span>{formatBytes(st.byte_rate)}/s</span>
        <span className={low ? "danger-text" : ""}>
          {formatBytes(st.free)} free · {formatHours(st.remaining)} left
        </span>
      </div>
      {st.error && (
        <p className="error" role="alert">
          {st.error}
        </p>
      )}
      {st.warnings?.map((w) => (
        <p key={w} className="warn-box" role="status">
          {w}
        </p>
      ))}
      {(st.state === "recording" || st.seconds > 0) && (
        <p className="muted">
          First sample {st.offset >= 0 ? "+" : ""}
          {st.offset.toFixed(3)} s after the start of the session{st.format ? ` · ${st.format}` : ""}
        </p>
      )}
      {(st.state !== "failed" || st.seconds > 0) && <TrackLevels tracks={st.tracks ?? []} peaks={st.peaks} />}
    </>
  );
}

function AudioState({ st }: { st: AudioStatus }) {
  switch (st.state) {
    case "recording":
      return (
        <span className="danger-text">
          <strong>● AUDIO</strong>
        </span>
      );
    case "starting":
      return <span className="note">starting…</span>;
    case "failed":
      return (
        <span className="danger-text">
          <strong>⚠ audio stopped</strong>
        </span>
      );
  }
  return <span className="muted">stopped</span>;
}

/** TrackLevels shows the peak of each track over the last second. */
function TrackLevels({ tracks, peaks }: { tracks: AudioTrack[]; peaks?: number[] }) {
  if (!tracks.length) return null;
  return (
    <div className="track-levels">
      {tracks.map((t, i) => {
        const db = peaks?.[i] === undefined ? -Infinity : toDbfs(peaks[i]);
        return (
          <div key={t.channel} className="track-level">
            <span className="track-name" title={t.file}>
              {trackLabel(t)}
            </span>
            <span className="track-bar" aria-hidden>
              <span className={"track-fill" + (db >= -0.5 ? " clip" : "")} style={{ width: `${meterFraction(db) * 100}%` }} />
            </span>
            <span className="track-db mono">{Number.isFinite(db) ? db.toFixed(0) : "−∞"}</span>
          </div>
        );
      })}
    </div>
  );
}

/**
 * TrackPicker lists the tracks of the device, named after what the console's
 * routing sends to them, to pick the ones to record: a click adds or removes
 * one and writes the list in Tracks to record ("1-16,23-24").
 */
function TrackPicker(props: { channels: number; names?: string[]; tracks: number[]; onChange(spec: string): void }) {
  const { channels, names, tracks, onChange } = props;
  if (channels < 1 || channels > 128) return null;
  const on = new Set(tracks);
  const all = Array.from({ length: channels }, (_, i) => i + 1);
  const named = all.filter((ch) => names?.[ch - 1]);
  const choose = (list: number[]) => onChange(formatTracks(list, channels));
  const toggle = (ch: number) => choose(on.has(ch) ? tracks.filter((t) => t !== ch) : [...tracks, ch]);
  return (
    <div className="track-pick">
      <div className="track-pick-head">
        <span className="field-label">
          Click the tracks to record{named.length ? ", named after what the console sends to its card" : ""}:
        </span>
        <button type="button" className="btn" onClick={() => choose(all)}>
          All
        </button>
        {named.length > 0 && named.length < channels && (
          <button type="button" className="btn" title="Only the tracks the console sends something named to" onClick={() => choose(named)}>
            Named
          </button>
        )}
      </div>
      <div className="track-pick-grid">
        {all.map((ch) => {
          const lit = on.has(ch);
          const name = names?.[ch - 1];
          return (
            <button
              key={ch}
              type="button"
              className={"btn track-pick-btn" + (lit ? " lit" : "")}
              aria-pressed={lit}
              disabled={lit && on.size === 1}
              title={`Track ${ch}${name ? `: ${name}` : ""}${lit ? " (recorded)" : ""}`}
              onClick={() => toggle(ch)}
            >
              <span className="mono">{String(ch).padStart(2, "0")}</span> {name || <span className="muted">—</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** LastCapture is how the audio of the last recording ended. */
function LastCapture({ st }: { st: AudioStatus }) {
  if (st.state !== "failed") return null;
  return (
    <p className="error" role="alert">
      Last recording: {st.error || "the audio stopped"}
      {st.seconds > 0 && ` (after ${formatDuration(st.seconds)} of audio)`}
    </p>
  );
}

/** CheckProblems says why the device configured would not record the console's tracks, with the console's card to use. */
export function CheckProblems({ audio }: { audio: Audio }) {
  const [error, setError] = useState("");
  const check = audio.view?.config.enabled ? audio.view.check : undefined;
  if (!check?.problems?.length) return null;
  const card = check.console;
  return (
    <div className="warn-box" role="status">
      {check.problems.map((p) => (
        <p key={p}>⚠ {p}</p>
      ))}
      {card && (
        <button
          type="button"
          className="btn primary"
          onClick={() => void toConsoleCard(audio).then(() => setError(""), (e: Error) => setError(e.message))}
        >
          Record from {card.name.replace(/, .*$/, "")} ({card.id}
          {card.channels ? `, ${card.channels} channels` : ""})
        </button>
      )}
      {error && <p className="error">{error}</p>}
    </div>
  );
}

function AudioSettings(props: { view: AudioView; audio: Audio; onSaved(v: AudioView): void; onRetry?: () => Promise<void> }) {
  const { view, audio, onSaved, onRetry } = props;
  const [cfg, setCfg] = useState<AudioConfig>(view.config);
  const [devices, setDevices] = useState<AudioDevice[] | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [test, setTest] = useState<AudioStatus | null>(null);
  const set = (patch: Partial<AudioConfig>) => setCfg((c) => ({ ...c, ...patch }));
  const dirty = JSON.stringify(cfg) !== JSON.stringify(view.config);
  // the page knows of changes not saved: starting a recording saves them
  const { setDraft } = audio;
  useEffect(() => {
    setDraft(dirty ? cfg : null);
  }, [dirty, cfg, setDraft]);
  useEffect(() => () => setDraft(null), [setDraft]);

  const tracks = parseTracks(cfg.tracks ?? "", cfg.channels);
  const rate = byteRate(cfg);
  const reserve = Math.max(256 * 2 ** 20, 60 * rate);
  const fits = view.free >= 0 && rate > 0 ? Math.max(0, (view.free - reserve) / rate) : -1;

  const run = async (what: string, f: () => Promise<void>) => {
    setBusy(what);
    setError("");
    try {
      await f();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  };
  const save = () =>
    run("save", async () => {
      const v = await audioApi.save(cfg);
      setCfg(v.config);
      onSaved(v);
    });
  const find = () => run("find", async () => setDevices(await audioApi.devices()));
  const retry = () =>
    run("retry", async () => {
      await onRetry?.(); // saves the changes first
    });
  const probe = () =>
    run("test", async () => {
      setTest(null);
      setTest(await audioApi.test(cfg));
    });
  const pick = (id: string) => {
    const d = devices?.find((x) => `${x.recorder} ${x.id}` === id);
    if (d) set({ recorder: d.recorder, device: d.id, ...(d.channels ? { channels: d.channels } : {}) });
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <label className="check">
        <input type="checkbox" checked={cfg.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        Record the audio tracks with every recording
      </label>
      {!dirty && onRetry && <CheckProblems audio={audio} />}
      <div className="row">
        <label>
          <span className="field-label">Recorder</span>
          <select className="select" value={cfg.recorder} onChange={(e) => set({ recorder: e.target.value as Recorder })}>
            {RECORDERS.map((r) => (
              <option key={r.id} value={r.id}>
                {r.label}
              </option>
            ))}
          </select>
        </label>
        <label className="grow">
          <span className="field-label">Device</span>
          <input
            className="input mono"
            type="text"
            value={cfg.device}
            placeholder={cfg.recorder === "arecord" ? "hw:CARD=X32,DEV=0" : "default"}
            onChange={(e) => set({ device: e.target.value })}
          />
        </label>
        <button type="button" className="btn" disabled={!!busy} onClick={() => void find()}>
          {busy === "find" ? "Looking…" : "Find devices"}
        </button>
      </div>
      {devices && (
        <div className="row">
          {devices.length ? (
            <label className="grow">
              <span className="field-label">Devices found</span>
              <select className="select" value="" onChange={(e) => pick(e.target.value)}>
                <option value="">Choose…</option>
                {devices.map((d) => (
                  <option key={`${d.recorder} ${d.id}`} value={`${d.recorder} ${d.id}`}>
                    {d.console ? "★ " : ""}
                    {d.name} — {d.id} ({d.recorder}
                    {d.channels ? `, ${d.channels} channels` : ""}
                    {d.format ? `, ${d.format}` : ""})
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <p className="muted">No capture device found: type its name.</p>
          )}
        </div>
      )}
      <div className="row">
        <label>
          <span className="field-label">Channels of the device</span>
          <input
            className="input num"
            type="number"
            min={1}
            max={128}
            value={cfg.channels}
            onChange={(e) => set({ channels: Number(e.target.value) || 0 })}
          />
        </label>
        <label>
          <span className="field-label">Tracks to record</span>
          <input
            className="input"
            type="text"
            value={cfg.tracks ?? ""}
            placeholder={`all (1-${cfg.channels}), or 1-16,23-24…`}
            aria-invalid={typeof tracks === "string"}
            onChange={(e) => set({ tracks: e.target.value })}
          />
        </label>
        <label>
          <span className="field-label">Bits</span>
          <select className="select" value={cfg.bits} onChange={(e) => set({ bits: Number(e.target.value) })}>
            {BITS.map((b) => (
              <option key={b} value={b}>
                {b}-bit
              </option>
            ))}
          </select>
        </label>
        <label>
          <span className="field-label">Rate</span>
          <select className="select" value={cfg.rate} onChange={(e) => set({ rate: Number(e.target.value) })}>
            {[...new Set([...RATES, cfg.rate])].map((r) => (
              <option key={r} value={r}>
                {r / 1000} kHz
              </option>
            ))}
          </select>
        </label>
      </div>
      <TrackPicker
        channels={cfg.channels}
        names={view.names}
        tracks={typeof tracks === "string" ? [] : tracks}
        onChange={(spec) => set({ tracks: spec })}
      />
      {typeof tracks === "string" ? (
        <p className="error">Tracks: {tracks}</p>
      ) : (
        <p className="muted">
          {tracks.length} WAV files ({cfg.bits}-bit), {formatBytes(rate)}/s: {formatBytes(rate * 3600)} per hour.{" "}
          {view.free >= 0 && (
            <span className={fits >= 0 && fits < 2 * 3600 ? "danger-text" : ""}>
              {formatBytes(view.free)} free in <code>{view.dir}</code>: about {formatHours(fits)} of audio.
            </span>
          )}
        </p>
      )}
      {cfg.recorder === "custom" && (
        <div className="row">
          <label className="grow">
            <span className="field-label">Command ({"{device}"}, {"{channels}"}, {"{rate}"} are replaced)</span>
            <input
              className="input mono"
              type="text"
              value={cfg.command ?? ""}
              placeholder={view.templates.arecord}
              onChange={(e) => set({ command: e.target.value })}
            />
          </label>
          <label>
            <span className="field-label">Raw samples (unless WAV)</span>
            <select className="select" value={cfg.input || "s32le"} onChange={(e) => set({ input: e.target.value })}>
              {INPUTS.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
      <label className="check">
        <input type="checkbox" checked={!!cfg.plain_names} onChange={(e) => set({ plain_names: e.target.checked })} />
        Name the files “Track 01”…, not after the console's routing (a device that is not the console's card)
      </label>
      {!dirty && view.command && (
        <p className="muted">
          Runs <code className="wrap">{view.command}</code>
        </p>
      )}
      <div className="row">
        <button type="submit" className="btn primary" disabled={!!busy || !dirty || typeof tracks === "string"}>
          {busy === "save" ? "Saving…" : dirty ? "Save" : "Saved"}
        </button>
        <button type="button" className="btn" disabled={!!busy || view.testing || typeof tracks === "string"} onClick={() => void probe()}>
          {busy === "test" ? "Listening for 3 s…" : "Test the input"}
        </button>
        {onRetry && (
          <button
            type="button"
            className="btn primary"
            disabled={!!busy || !cfg.enabled || typeof tracks === "string"}
            onClick={() => void retry()}
          >
            {busy === "retry" ? "Starting…" : dirty ? "Save and start the audio" : "Start the audio"}
          </button>
        )}
      </div>
      {dirty && !onRetry && (
        <p className="note">Not saved yet: Save, or Start recording saves these settings first (a recording uses the saved ones).</p>
      )}
      {(error || view.error) && (
        <p className="error" role="alert">
          {error || view.error}
        </p>
      )}
      {test && <TestResult st={test} />}
    </form>
  );
}

function TestResult({ st }: { st: AudioStatus }) {
  const nothing = st.seconds === 0; // the warnings say why
  const silent = !nothing && st.peaks?.every((p) => p === 0);
  return (
    <div className="audio-test">
      <h4>Test of the input</h4>
      {st.error ? (
        <p className="error" role="alert">
          {st.error}
        </p>
      ) : (
        !nothing && (
          <p>
          {st.format || "no stream"} · {formatDuration(st.seconds)} received
            {silent && <span className="note"> · silence on every track</span>}
          </p>
        )
      )}
      {st.warnings?.map((w) => (
        <p key={w} className="warn-box">
          {w}
        </p>
      ))}
      <TrackLevels tracks={st.tracks ?? []} peaks={st.peaks} />
    </div>
  );
}

const EXPORTS: { format: ExportFormat; label: string; title: string }[] = [
  { format: "reaper", label: "Reaper project", title: "A .rpp with one track per audio file, and the markers" },
  { format: "audacity", label: "Audacity labels", title: "A label track: File › Import › Labels…" },
  { format: "csv", label: "CSV", title: "Every change and marker, with its time in the audio" },
];

/** SessionAudio describes a session's audio and offers its exports. */
export function SessionAudio({ detail }: { detail: SessionDetail }) {
  const a = detail.meta.audio;
  return (
    <>
      <h4>Audio</h4>
      {a ? (
        <>
          <dl className="facts">
            <dt>Tracks</dt>
            {a.frames > 0 || !a.error ? (
              <dd>
                {a.tracks?.length ?? 0} files in <code>audio/</code> ({a.bits}-bit, {a.rate / 1000} kHz)
                {a.frames > 0 && `, ${formatDuration(a.frames / a.rate)}`}
              </dd>
            ) : (
              <dd className="danger-text">
                nothing recorded: the files in <code>audio/</code> are empty
              </dd>
            )}
            {a.frames > 0 && (
              <>
                <dt>Sync</dt>
                <dd>
                  first sample at {a.offset.toFixed(3)} s{a.drift_ppm ? `, clock drift ${a.drift_ppm.toFixed(1)} ppm (corrected)` : ""}
                  {a.overruns ? <span className="danger-text"> · {a.overruns} overrun(s): samples were lost</span> : null}
                </dd>
              </>
            )}
          </dl>
          {a.error && <p className="error">{a.error}</p>}
        </>
      ) : (
        <p className="muted">No audio recorded with this session.</p>
      )}
      <div className="row exports">
        {EXPORTS.map((e) => (
          <a key={e.format} className="btn" href={exportUrl(detail.name, e.format)} download title={e.title}>
            ⤓ {e.label}
          </a>
        ))}
      </div>
      {a && (
        <p className="muted">
          The session folder already holds <code>{detail.name}.rpp</code> and <code>labels.txt</code>, next to{" "}
          <code>audio/</code>: open the project from there, or import the WAV files in Audacity, then the labels (see the{" "}
          <a href="https://yoda-jm.github.io/tabula/recording.html">recording guide</a>).
        </p>
      )}
    </>
  );
}
