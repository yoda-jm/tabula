// Recording and replay (docs/API.md, "Recording and replay").

import { LONG, SLOW, call } from "./api";
import type { AudioInfo, AudioStatus } from "./audio";
import { formatTracks } from "./audio";

export interface SessionMeta {
  format?: number;
  start: string;
  end?: string;
  console?: string;
  note?: string;
  events: number;
  markers: number;
  duration: number; // seconds
  /** the audio tracks recorded with the session */
  audio?: AudioInfo;
}

export interface Record {
  recording: boolean;
  name?: string;
  meta?: SessionMeta;
  replaying: boolean;
  replay_of?: string;
  replay_at?: number;
  error?: string;
  /** the audio capture of the recording (or of the last one) */
  audio?: AudioStatus;
}

export interface SessionEvent {
  t: number;
  addr?: string;
  text?: string;
  value?: number | string;
  origin?: string;
  marker?: string;
}

export interface SessionSummary {
  name: string;
  meta: SessionMeta;
}

export interface SessionDetail extends SessionSummary {
  markers?: SessionEvent[];
  /** node lines of the console at the start (start.scn and start.nodes) */
  snapshot?: number;
  /** whether start.scn, the console at the start as a scene file, is there */
  scene?: boolean;
  /** lines of the change log (events.jsonl, from 1) that could not be read: their changes are missing */
  damaged?: number[];
}

export const sessionApi = {
  record: () => call<Record>("GET", "/api/record"),
  // starting waits for the audio to run (3 s at most), stopping for its files to be finished; a session's
  // details, a replay and a deletion read or remove all of a show's files
  start: (note: string) => call<Record>("POST", "/api/record/start", { note }, LONG),
  mark: (label: string) => call<Record>("POST", "/api/record/mark", { label }),
  stop: () => call<Record>("POST", "/api/record/stop", {}, LONG),
  sessions: () => call<SessionSummary[]>("GET", "/api/sessions", undefined, SLOW),
  session: (name: string) => call<SessionDetail>("GET", "/api/sessions/" + encodeURIComponent(name), undefined, LONG),
  remove: (name: string) => call<void>("DELETE", "/api/sessions/" + encodeURIComponent(name), undefined, LONG),
  replay: (session: string, from: number, speed: number) =>
    call<Record>("POST", "/api/replay/start", { session, from, speed }, LONG),
  stopReplay: () => call<Record>("POST", "/api/replay/stop", {}),
};

/** formatDuration prints seconds as h:mm:ss or m:ss. */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

/** damagedText says which lines of a session's change log could not be read, or "" when every line could. */
export function damagedText(lines: number[] | undefined): string {
  if (!lines?.length) return "";
  const one = lines.length === 1;
  const which = formatTracks(lines, Math.max(...lines) + 1).replace(/,/g, ", "); // one past the last: never "all"
  return one
    ? `1 line of the change log could not be read: its changes are missing from this session (line ${which}).`
    : `${lines.length} lines of the change log could not be read: their changes are missing from this session (lines ${which}).`;
}

/** A question asked before something that ends the recording. */
export interface EndsRecording {
  title: string;
  body: string;
  confirm: string;
}

/**
 * endsRecording is the question asked before disconnecting or connecting to
 * a console (another one or the same) while a recording runs: tabula ends
 * the recording, audio included. null when nothing is recorded.
 */
export function endsRecording(action: "connect" | "disconnect", rec: Record | null): EndsRecording | null {
  if (!rec?.recording) return null;
  return action === "disconnect"
    ? { title: "Disconnect while recording?", body: "A recording is running: disconnecting ends it, with its audio.", confirm: "Disconnect" }
    : { title: "Connect while recording?", body: "Connecting to another console ends the recording, with its audio.", confirm: "Connect" };
}

/** addsMarker tells whether a key press adds a marker: M without Ctrl, Cmd or Alt, once however long it is held. */
export function addsMarker(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; repeat: boolean }): boolean {
  return e.key.toLowerCase() === "m" && !e.ctrlKey && !e.metaKey && !e.altKey && !e.repeat;
}

/** parseTime reads "90", "1:30" or "1:02:03" as seconds; NaN if unreadable. */
export function parseTime(text: string): number {
  const t = text.trim();
  if (!t) return 0;
  const parts = t.split(":");
  if (parts.length > 3 || parts.some((p) => !/^\d+(\.\d+)?$/.test(p))) return NaN;
  return parts.reduce((acc, p) => acc * 60 + Number(p), 0);
}
