// Audio tracks recorded with the sessions (docs/API.md, "Audio";
// docs/recording.md).

import { LONG, call } from "./api";
import type { Record as RecordStatus } from "./session";

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
  /** when audio is on: why the device configured would not record the console's tracks */
  check?: AudioCheck;
}

/** What the machine says of the device configured (docs/API.md, "Audio"). */
export interface AudioCheck {
  problems?: string[];
  /** the console's card, connected, when it is not the device configured */
  console?: AudioDevice;
}

export interface AudioDevice {
  recorder: Recorder;
  id: string;
  name: string;
  /** capture channels, when known: the most */
  channels?: number;
  /** the fewest, when the card takes a range (2 to 4) */
  min_channels?: number;
  format?: string;
  rates?: string;
  console?: boolean;
}

/** channelRange is how many channels a device records: "32", "2 to 4". */
export const channelRange = (d: AudioDevice) =>
  d.min_channels && d.min_channels < (d.channels ?? 0) ? `${d.min_channels} to ${d.channels}` : String(d.channels ?? "");

/**
 * tooManyChannels says why the settings ask more channels than the device
 * has, when it is among the devices found and tells its channels (the
 * server refuses to save them); otherwise "".
 */
export function tooManyChannels(c: AudioConfig, devices: AudioDevice[] | null): string {
  const d = devices?.find((x) => x.recorder === c.recorder && x.id === c.device);
  if (!d?.channels || c.channels <= d.channels) return "";
  const card = devices?.find((x) => x.console && x.recorder === d.recorder && x !== d);
  return (
    `${d.name} has ${channelRange(d)} input channels, not ${c.channels}: set Channels of the device to ${d.channels}` +
    (card ? `, or choose the console's card (★ ${card.name}).` : ".")
  );
}

const FIELDS: { key: keyof AudioConfig; label: string; show?(c: AudioConfig): string }[] = [
  { key: "enabled", label: "Audio tracks", show: (c) => (c.enabled ? "recorded" : "off") },
  { key: "recorder", label: "Recorder" },
  { key: "device", label: "Device", show: (c) => c.device || "default" },
  { key: "channels", label: "Channels of the device" },
  { key: "tracks", label: "Tracks", show: (c) => c.tracks || "all" },
  { key: "bits", label: "Bits", show: (c) => `${c.bits}-bit` },
  { key: "rate", label: "Rate", show: (c) => `${c.rate / 1000} kHz` },
  { key: "command", label: "Command" },
  { key: "input", label: "Raw samples" },
  { key: "plain_names", label: "File names", show: (c) => (c.plain_names ? "Track 01…" : "from the routing") },
];

/**
 * sameConfig tells whether the page's settings are the saved ones, field by
 * field: the server leaves empty fields out (no tracks: all of them).
 */
export const sameConfig = (saved: AudioConfig, draft: AudioConfig) => configChanges(saved, draft).length === 0;

/** configChanges lists what differs between the saved settings and the page's: "Device: hw:… → hw:…". */
export function configChanges(saved: AudioConfig, draft: AudioConfig): { label: string; from: string; to: string }[] {
  return FIELDS.flatMap((f) => {
    const show = f.show ?? ((c: AudioConfig) => String(c[f.key] ?? ""));
    const [from, to] = [show(saved), show(draft)];
    return from === to ? [] : [{ label: f.label, from: from || "—", to: to || "—" }];
  });
}

export type ExportFormat = "reaper" | "audacity" | "csv" | "scene";

export const audioApi = {
  get: () => call<AudioView>("GET", "/api/audio"),
  // saving and listing the devices probe the cards, a test records 3 s, a restart waits for the audio to start
  save: (c: AudioConfig) => call<AudioView>("POST", "/api/audio/config", c, LONG),
  devices: () => call<AudioDevice[]>("GET", "/api/audio/devices", undefined, LONG),
  test: (c: AudioConfig) => call<AudioStatus>("POST", "/api/audio/test", c, LONG),
  /** starts the audio of the recording again, after it failed before recording anything */
  restart: () => call<RecordStatus>("POST", "/api/audio/restart", {}, LONG),
};

/** audioTrouble says what is wrong with the audio of a recording: failed (nothing recorded), a warning, or null. */
export function audioTrouble(st: AudioStatus | undefined): { level: "failed" | "warn"; text: string } | null {
  if (!st) return null;
  if (st.state === "failed") return { level: "failed", text: st.error || "the audio stopped" };
  if (st.warnings?.length) return { level: "warn", text: st.warnings.join(" · ") };
  return null;
}

/** audioSummary is the audio of a session in a few words: "audio: 18 tracks", "audio failed", "no audio". */
export function audioSummary(a: AudioInfo | undefined | null): string {
  if (!a) return "no audio";
  if (a.error && !a.frames) return "audio failed";
  return `audio: ${plural(a.tracks?.length ?? 0, "track")}${a.error ? ", cut short" : ""}`;
}

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
