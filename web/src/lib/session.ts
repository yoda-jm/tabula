// Recording and replay (docs/API.md, "Recording and replay").

import { call } from "./api";
import type { AudioInfo, AudioStatus } from "./audio";

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
}

export const sessionApi = {
  record: () => call<Record>("GET", "/api/record"),
  start: (note: string) => call<Record>("POST", "/api/record/start", { note }),
  mark: (label: string) => call<Record>("POST", "/api/record/mark", { label }),
  stop: () => call<Record>("POST", "/api/record/stop", {}),
  sessions: () => call<SessionSummary[]>("GET", "/api/sessions"),
  session: (name: string) => call<SessionDetail>("GET", "/api/sessions/" + encodeURIComponent(name)),
  remove: (name: string) => call<void>("DELETE", "/api/sessions/" + encodeURIComponent(name)),
  replay: (session: string, from: number, speed: number) =>
    call<Record>("POST", "/api/replay/start", { session, from, speed }),
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

/** parseTime reads "90", "1:30" or "1:02:03" as seconds; NaN if unreadable. */
export function parseTime(text: string): number {
  const t = text.trim();
  if (!t) return 0;
  const parts = t.split(":");
  if (parts.length > 3 || parts.some((p) => !/^\d+(\.\d+)?$/.test(p))) return NaN;
  return parts.reduce((acc, p) => acc * 60 + Number(p), 0);
}
