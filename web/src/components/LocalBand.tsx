// The Local replay band, under the top bar on every page while a session
// plays back on this machine: where it is, the transport, what is heard,
// and the way back to the console.

import { useEffect, useRef, useState } from "react";
import type { LocalStatus, MixTrack, Status } from "../lib/api";
import type { AudioTrack } from "../lib/audio";
import { SPEEDS, listenChoices, listenKey, localApi, localAt } from "../lib/local";
import { formatDuration, sessionApi } from "../lib/session";
import { useStatus } from "../store/hooks";
import { store } from "../store/store";

export function LocalBand() {
  const local = useStatus().local;
  if (!local) return null;
  return <Band key={local.session} l={local} />;
}

function Band({ l }: { l: LocalStatus }) {
  const [now, setNow] = useState(Date.now());
  const [drag, setDrag] = useState<number | null>(null); // the seek bar being dragged
  const [tracks, setTracks] = useState<AudioTrack[]>([]);
  const [busy, setBusy] = useState(false);
  const [picking, setPicking] = useState(false);

  // the time moves on while playing
  useEffect(() => {
    if (!l.playing) return;
    const id = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(id);
  }, [l.playing]);

  // the session's tracks, for the Listen menu
  useEffect(() => {
    sessionApi.session(l.session).then(
      (d) => setTracks(d.meta.audio?.tracks ?? []),
      () => setTracks([]),
    );
  }, [l.session]);

  const act = async (f: () => Promise<Status>) => {
    setBusy(true);
    try {
      store.setStatus(await f());
    } catch (e) {
      store.toast(`Local replay: ${(e as Error).message}`, "error");
    } finally {
      setBusy(false);
    }
  };
  const at = localAt(l, now);
  const seek = (t: number) => act(() => localApi.transport({ at: Math.min(l.duration, Math.max(0, t)) }));
  const toggle = () => act(() => localApi.transport({ play: !l.playing, ...(at >= l.duration ? { at: 0 } : {}) }));

  // Space plays or pauses, ← → move by 5 s (not in a field)
  const keys = useRef({ toggle, seek, at });
  keys.current = { toggle, seek, at };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if ((e.target as HTMLElement).closest?.("input, textarea, select, button, [contenteditable]")) return;
      if (e.key === " ") {
        e.preventDefault();
        if (!e.repeat) void keys.current.toggle();
      } else if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
        e.preventDefault();
        void keys.current.seek(keys.current.at + (e.key === "ArrowLeft" ? -5 : 5));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const choices = listenChoices(l, tracks);
  const key = listenKey(l);
  const silentSpeed = l.listen !== "off" && l.speed !== 1;

  return (
    <div className="local-band" role="region" aria-label="Local replay">
      <strong className="local-title" title="The console is the session's: nothing reaches the real console">
        Local replay · {l.session}
      </strong>
      <span className="local-transport">
        <button type="button" className="btn small" disabled={busy} onClick={() => void seek(at - 5)} title="Back 5 s (←)" aria-label="Back 5 seconds">
          ⏪
        </button>
        <button type="button" className="btn small local-play" disabled={busy} onClick={() => void toggle()} title="Play or pause (Space)">
          {l.playing ? "❚❚ Pause" : "▶ Play"}
        </button>
        <button type="button" className="btn small" disabled={busy} onClick={() => void seek(at + 5)} title="On 5 s (→)" aria-label="Forward 5 seconds">
          ⏩
        </button>
      </span>
      <input
        className="local-seek"
        type="range"
        min={0}
        max={l.duration || 1}
        step={0.1}
        value={drag ?? at}
        aria-label="Position in the session"
        onChange={(e) => setDrag(Number(e.target.value))}
        onPointerUp={() => {
          if (drag !== null) void seek(drag);
          setDrag(null);
        }}
        onKeyUp={() => {
          if (drag !== null) void seek(drag);
          setDrag(null);
        }}
      />
      <span className="local-time">
        {formatDuration(drag ?? at)} / {formatDuration(l.duration)}
      </span>
      <select
        className="select small"
        value={l.speed}
        disabled={busy}
        aria-label="Speed"
        onChange={(e) => void act(() => localApi.transport({ speed: Number(e.target.value) }))}
      >
        {SPEEDS.map((s) => (
          <option key={s} value={s}>
            {s}×
          </option>
        ))}
      </select>
      {l.audio && (
        <label className="local-listen">
          <span>Listen</span>
          <select
            className="select small"
            value={key}
            disabled={busy}
            onChange={(e) => {
              if (e.target.value === "pick") return setPicking(true);
              const c = choices.find((x) => x.key === e.target.value);
              if (c) void act(() => localApi.listen(c.body));
            }}
          >
            {key === "custom" && <option value="custom">{l.tracks?.length} tracks</option>}
            {choices.map((c) => (
              <option key={c.key} value={c.key}>
                {c.label}
              </option>
            ))}
            <option value="pick">Choose tracks…</option>
          </select>
        </label>
      )}
      {picking && (
        <TrackPicker
          tracks={tracks.filter((t) => t.file)}
          chosen={l.listen === "stereo" ? stereoMix(l) : (l.tracks ?? [])}
          onCancel={() => setPicking(false)}
          onApply={(mix) => {
            setPicking(false);
            void act(() => localApi.listen({ tracks: mix }));
          }}
        />
      )}
      {!!l.cue?.length && (
        <span
          className="local-cue"
          title="CUE: SOLO sends the raw track as recorded to the phones (after the headamp, before EQ, dynamics and fader: earlier than a PFL), instead of the Listen choice until no solo is left"
        >
          CUE · raw{" "}
          {l.cue
            .map((m) => {
              const t = tracks.find((x) => x.channel === m.track);
              return `${m.track}${t?.name ? " " + t.name : ""}`;
            })
            .join(", ")}
        </span>
      )}
      {silentSpeed && <span className="local-note">silent at {l.speed}× (1× to listen)</span>}
      {l.error && <span className="local-note error">{l.error}</span>}
      <button type="button" className="btn small local-back" disabled={busy} onClick={() => void act(() => localApi.close())}>
        {l.back ? "Back to the console" : "Close"}
      </button>
    </div>
  );
}

/** stereoMix is the stereo out as tracks, to start a choice from it. */
const stereoMix = (l: LocalStatus): MixTrack[] => [
  { track: l.main[0], gain: 1, pan: -1 },
  { track: l.main[1], gain: 1, pan: 1 },
];

const PANS: { label: string; pan: number }[] = [
  { label: "L", pan: -1 },
  { label: "C", pan: 0 },
  { label: "R", pan: 1 },
];

/** TrackPicker chooses the tracks heard, each left, centre or right. */
function TrackPicker(props: { tracks: AudioTrack[]; chosen: MixTrack[]; onApply(mix: MixTrack[]): void; onCancel(): void }) {
  const [mix, setMix] = useState<Map<number, number>>(() => new Map(props.chosen.map((m) => [m.track, m.pan])));
  const set = (track: number, pan: number | null) =>
    setMix((prev) => {
      const next = new Map(prev);
      if (pan === null) next.delete(track);
      else next.set(track, pan);
      return next;
    });
  return (
    <div className="local-picker" role="dialog" aria-label="Tracks to listen to">
      <div className="local-picker-list">
        {props.tracks.map((t) => {
          const pan = mix.get(t.channel);
          return (
            <div key={t.channel} className="local-picker-row">
              <label>
                <input type="checkbox" checked={pan !== undefined} onChange={(e) => set(t.channel, e.target.checked ? 0 : null)} />{" "}
                {t.channel}
                {t.name ? ` · ${t.name}` : ""}
              </label>
              <span className="seg" role="group" aria-label={`Track ${t.channel}: pan`}>
                {PANS.map((p) => (
                  <button
                    key={p.label}
                    type="button"
                    className={"btn small seg-btn" + (pan === p.pan ? " lit" : "")}
                    aria-pressed={pan === p.pan}
                    onClick={() => set(t.channel, p.pan)}
                  >
                    {p.label}
                  </button>
                ))}
              </span>
            </div>
          );
        })}
      </div>
      <div className="row">
        <button
          type="button"
          className="btn primary small"
          onClick={() => props.onApply([...mix].map(([track, pan]) => ({ track, gain: 1, pan })))}
        >
          Listen to {mix.size} track{mix.size === 1 ? "" : "s"}
        </button>
        <button type="button" className="btn small" onClick={props.onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}
