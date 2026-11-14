// Compressing a session's tracks to FLAC in place: lossless, about half the
// size, in the background; every replay and export reads them as WAV ones
// (docs/recording.md, "Compressing the tracks").

import type { AudioInfo } from "./audio";
import { call } from "./api";

export interface Compress {
  /** the sessions waiting, the one compressed first */
  queue: string[];
  session?: string;
  track?: string;
  done: number;
  total: number;
  /** bytes saved since tabula started */
  saved: number;
  /** "recording" or "in use": waiting */
  paused?: string;
  error?: string;
}

export const compressApi = {
  get: () => call<Compress>("GET", "/api/compress"),
  start: (session: string) => call<Compress>("POST", "/api/sessions/" + encodeURIComponent(session) + "/compress", {}),
};

/** trackFormats counts a session's track files still WAV and already FLAC. */
export function trackFormats(a?: AudioInfo): { wav: number; flac: number } {
  let wav = 0;
  let flac = 0;
  for (const t of a?.tracks ?? []) {
    if (!t.file) continue;
    if (/\.flac$/i.test(t.file)) flac++;
    else if (/\.wav$/i.test(t.file)) wav++;
  }
  return { wav, flac };
}

/** compressLine says where the compression of a session is, "" when it is not queued. */
export function compressLine(c: Compress | null, session: string): string {
  if (!c) return "";
  const at = c.queue.indexOf(session);
  if (at < 0) return "";
  if (at > 0) return `waiting: ${at} session${at > 1 ? "s" : ""} before it`;
  if (c.paused === "recording") return "paused while recording";
  if (c.paused === "in use") return "waiting while it is replayed or copied";
  if (c.total === 0) return "starting…";
  return `track ${c.done + 1} of ${c.total}${c.track ? ` (${c.track})` : ""}`;
}
