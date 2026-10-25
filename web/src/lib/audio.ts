// Audio tracks recorded with the sessions (docs/API.md, "Audio";
// docs/recording.md).

import { call } from "./api";

export type Recorder = "arecord" | "parecord" | "ffmpeg" | "custom";

export interface AudioConfig {
  enabled: boolean;
  recorder: Recorder;
  device: string;
  /** channels the device delivers, recorded or not */
  channels: number;
  rate: number;
  /** channels written to files, e.g. "1-16"; empty for all */
  tracks?: string;
  bits: number;
  plain_names?: boolean;
  command?: string;
  input?: string;
}

export interface AudioTrack {
  channel: number;
  name?: string;
  /** relative to the session: "audio/01 Hang.wav" */
  file?: string;
}

/** The audio section of a session's meta.json. */
export interface AudioInfo {
  rate: number;
  bits: number;
  channels: number;
  tracks: AudioTrack[] | null;
  /** seconds from the session start to the first sample */
  offset: number;
  start?: string;
  drift_ppm?: number;
  frames: number;
  device?: string;
  command?: string;
  overruns?: number;
  error?: string;
}

export type AudioState = "starting" | "recording" | "stopped" | "failed";

export interface AudioStatus extends AudioInfo {
  state: AudioState;
  warnings?: string[];
  format?: string;
  seconds: number;
  bytes: number;
  byte_rate: number;
  /** bytes free on the disk, -1 if unknown */
  free: number;
  /** seconds of audio that still fit, -1 if unknown */
  remaining: number;
  /** linear peak levels of the tracks over the last second */
  peaks?: number[];
}

export interface AudioView {
  config: AudioConfig;
  command?: string;
  error?: string;
  templates: Record<string, string>;
  byte_rate: number;
  dir: string;
  free: number;
  remaining: number;
  status?: AudioStatus;
  testing: boolean;
  /** what the console's routing sends to each track of its card ("" when nothing named), while connected */
  names?: string[];
}

export interface AudioDevice {
  recorder: Recorder;
  id: string;
  name: string;
  channels?: number;
  format?: string;
  rates?: string;
  console?: boolean;
}

export type ExportFormat = "reaper" | "audacity" | "csv" | "scene";

export const audioApi = {
  get: () => call<AudioView>("GET", "/api/audio"),
  save: (c: AudioConfig) => call<AudioView>("POST", "/api/audio/config", c),
  devices: () => call<AudioDevice[]>("GET", "/api/audio/devices"),
  test: (c: AudioConfig) => call<AudioStatus>("POST", "/api/audio/test", c),
};

/** exportUrl downloads an export of a session. */
export const exportUrl = (session: string, format: ExportFormat) =>
  `/api/sessions/${encodeURIComponent(session)}/export?format=${format}`;

/**
 * parseTracks reads "1-16" or "1-8, 12" (empty or "all": every channel) as
 * sorted channel numbers, as the server does; a string says what is wrong.
 */
export function parseTracks(spec: string, channels: number): number[] | string {
  const s = spec.trim();
  if (s === "" || s.toLowerCase() === "all") return Array.from({ length: channels }, (_, i) => i + 1);
  const seen = new Set<number>();
  for (const part of s.split(/[,; ]+/).filter(Boolean)) {
    const m = /^(\d+)(?:-(\d+))?$/.exec(part);
    if (!m) return `“${part}” is not a channel or a range`;
    const a = Number(m[1]);
    const b = m[2] === undefined ? a : Number(m[2]);
    if (a < 1 || b > channels || a > b) return `${part} is outside 1-${channels}`;
    for (let ch = a; ch <= b; ch++) seen.add(ch);
  }
  if (!seen.size) return "no channel";
  return [...seen].sort((x, y) => x - y);
}

/** formatTracks writes channels back as parseTracks reads them: [1…16, 23, 24] → "1-16,23-24"; all of them → "". */
export function formatTracks(tracks: number[], channels: number): string {
  const t = [...new Set(tracks)].filter((ch) => ch >= 1 && ch <= channels).sort((x, y) => x - y);
  if (t.length === channels) return "";
  const parts: string[] = [];
  for (let i = 0; i < t.length; ) {
    let j = i;
    while (j + 1 < t.length && t[j + 1] === t[j] + 1) j++;
    parts.push(j > i ? `${t[i]}-${t[j]}` : `${t[i]}`);
    i = j + 1;
  }
  return parts.join(",");
}

/** audioPlan says what audio a configuration records: "16 tracks, 24-bit WAV", or "" when none. */
export function audioPlan(c: AudioConfig | undefined): string {
  if (!c?.enabled) return "";
  const t = parseTracks(c.tracks ?? "", c.channels);
  if (typeof t === "string") return "";
  return `${plural(t.length, "track")}, ${c.bits}-bit WAV`;
}

/** plural counts things: "1 track", "16 tracks". */
export const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** byteRate is what a configuration writes per second, all files. */
export function byteRate(c: AudioConfig): number {
  const t = parseTracks(c.tracks ?? "", c.channels);
  return typeof t === "string" ? 0 : t.length * c.rate * (c.bits / 8);
}

/** formatBytes writes a size as people read it: "12.4 GB". */
export function formatBytes(n: number): string {
  if (n < 0) return "unknown";
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e7) return `${Math.round(n / 1e6)} MB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)} MB`;
  return `${Math.round(n / 1e3)} kB`;
}

/** formatHours writes a long duration: "3 h 20 min", "45 min". */
export function formatHours(seconds: number): string {
  if (seconds < 0) return "unknown";
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")} min`;
}

/** A remaining recording time below this is a warning (as the server's). */
export const LOW_DISK_SECONDS = 20 * 60;

/** trackLabel names a track as its file does: "01 Hang" or "Track 01". */
export function trackLabel(t: AudioTrack): string {
  const n = String(t.channel).padStart(2, "0");
  if (t.file) return t.file.replace(/^.*\//, "").replace(/\.wav$/i, "");
  return t.name ? `${n} ${t.name}` : `Track ${n}`;
}
