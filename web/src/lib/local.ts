// Local replay: a session played back on this machine, without the
// console (docs/recording.md, "Replaying locally"). tabula connects to a
// session console holding the session's state; every page follows it.

import type { LocalStatus, MixTrack, Status } from "./api";
import { call } from "./api";

export const localApi = {
  open: (session: string, from: number) => call<Status>("POST", "/api/local/open", { session, from }),
  /** transport plays, pauses, seeks (at, seconds) and sets the speed */
  transport: (t: { play?: boolean; at?: number; speed?: number }) => call<Status>("POST", "/api/local/transport", t),
  listen: (body: { preset: "stereo" | "off" } | { tracks: MixTrack[] }) => call<Status>("PUT", "/api/local/listen", body),
  close: () => call<Status>("POST", "/api/local/close", {}),
};

/** localAt is where the replay is at now (Unix ms), moving on from the status while it plays. */
export function localAt(l: LocalStatus, now: number): number {
  if (!l.playing) return l.at;
  return Math.min(l.duration, Math.max(0, l.at + ((now - l.at_ms) / 1000) * l.speed));
}

export const SPEEDS = [0.5, 1, 2, 4, 8];

/** A ListenChoice is an entry of the Listen menu: off, the stereo out, or one track in both ears. */
export interface ListenChoice {
  key: string;
  label: string;
  body: { preset: "stereo" | "off" } | { tracks: MixTrack[] };
}

/**
 * listenChoices lists what can be listened to: off, the stereo out when the
 * card recorded main L and R, and each recorded track alone.
 */
export function listenChoices(l: LocalStatus, tracks: { channel: number; name?: string; file?: string }[]): ListenChoice[] {
  const out: ListenChoice[] = [{ key: "off", label: "Nothing", body: { preset: "off" } }];
  const recorded = tracks.filter((t) => t.file);
  const has = (n: number) => recorded.some((t) => t.channel === n);
  if (l.main[0] && l.main[1] && has(l.main[0]) && has(l.main[1])) {
    out.push({ key: "stereo", label: `Stereo out (tracks ${l.main[0]} and ${l.main[1]})`, body: { preset: "stereo" } });
  }
  for (const t of recorded) {
    out.push({
      key: `t${t.channel}`,
      label: `Track ${t.channel}${t.name ? " · " + t.name : ""}`,
      body: { tracks: [{ track: t.channel, gain: 1, pan: 0 }] },
    });
  }
  return out;
}

/** listenKey is the Listen menu's entry for what plays now. */
export function listenKey(l: LocalStatus): string {
  if (l.listen === "stereo") return "stereo";
  if (l.listen === "tracks" && l.tracks?.length === 1) return `t${l.tracks[0].track}`;
  return l.listen === "off" ? "off" : "custom";
}
