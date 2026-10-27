// The USB stick in the console: its current directory, read with
// GET /api/usb, and its player (/-stat/tape), driven with POST /api/usb
// (docs/API.md, "USB stick").

import { LONG, call } from "./api";

export type UsbKind =
  | "parent"
  | "dir"
  | "wav"
  | "scene"
  | "snippet"
  | "show"
  | "effect"
  | "routing"
  | "channel"
  | "monitor"
  | "prefs"
  | "volume"
  | "file";

export interface UsbEntry {
  /** as the console numbers it, from 1 */
  index: number;
  /** without the brackets of directories */
  name: string;
  kind: UsbKind;
}

export interface UsbView {
  mounted: boolean;
  /** the current directory's name, "" at the root */
  path: string;
  /** the file selected */
  title: string;
  pos: number;
  entries: UsbEntry[];
}

/** ff and rew wind the player until play, pause or stop (or an end of the file) */
export type UsbOp = "play" | "pause" | "stop" | "ff" | "rew" | "prev" | "next";

export const usbApi = {
  // the console reads its stick slowly: tabula gives it 10 to 15 s
  get: () => call<UsbView>("GET", "/api/usb", undefined, LONG),
  /** open enters a directory, plays a WAV file or loads a file; it answers the listing after */
  open: (index: number) => call<UsbView>("POST", "/api/usb", { op: "open", index }, LONG),
  op: (op: UsbOp) => call<void>("POST", "/api/usb", { op }, LONG),
};

/** What opening an entry does: the button's label, and whether it changes the mix (asked first). */
export function entryAction(kind: UsbKind): { label: string; asks?: boolean } | null {
  switch (kind) {
    case "parent":
    case "dir":
      return { label: "Open" };
    case "wav":
      return { label: "▶ Play" };
    case "scene":
    case "snippet":
    case "effect":
    case "routing":
    case "channel":
    case "monitor":
    case "show":
      return { label: "Load…", asks: true };
  }
  return null; // preferences, other files: not from here
}

/** loadWhat says what loading a file of the stick changes, before it is loaded. */
export function loadWhat(kind: UsbKind): string {
  switch (kind) {
    case "scene":
      return "the whole mix changes at once";
    case "snippet":
      return "the parameters the snippet holds change at once";
    case "channel":
      return "the console applies the channel preset to its selected strip";
    case "effect":
      return "the console applies the effect preset to an effect slot (the one shown on its Effects screen)";
    case "routing":
      return "the console's routing (inputs, outputs, card) changes at once";
    case "monitor":
      return "the AES50 / DP48 personal monitor settings change at once";
    case "show":
      return "the console's show is replaced: its cues, scenes and snippets (the mix itself does not change)";
  }
  return "the parameters it holds change at once";
}

/** winding tells whether the player winds forward or back (/-stat/tape/state FF, REW). */
export const winding = (state: string) => state === "FF" || state === "REW";

export const KIND_LABEL: Record<UsbKind, string> = {
  parent: "up",
  dir: "folder",
  wav: "WAV",
  scene: "scene",
  snippet: "snippet",
  show: "show",
  effect: "effect",
  routing: "routing",
  channel: "channel",
  monitor: "AES50/DP48",
  prefs: "prefs",
  volume: "system",
  file: "file",
};

/** formatClock shows seconds as the console does: 3:24, 1:01:40. */
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

/** The player's state (/-stat/tape/state) in words. */
export const TAPE_STATES: Record<string, string> = {
  STOP: "stopped",
  PPAUSE: "paused",
  PLAY: "playing",
  RPAUSE: "recording paused",
  RECORD: "recording",
  FF: "fast forward",
  REW: "rewind",
};

/** dropouts is the number of dropouts of a recording to the stick: the console tells them in rtime, as −(dropouts + 1). */
export const dropouts = (rtime: number) => (rtime < -1 ? -rtime - 1 : 0);

/** recordsToStick tells whether the console records to its stick (/-stat/tape/state RECORD, or paused): Stop asks first. */
export const recordsToStick = (state: string) => state === "RECORD" || state === "RPAUSE";

/** isPlaying tells whether the player's file is this entry of the directory shown. */
export function isPlaying(file: string, path: string, name: string): boolean {
  const parts = file.split("/").filter(Boolean);
  if (!parts.length || parts[parts.length - 1] !== name) return false;
  return (parts[parts.length - 2] ?? "") === path;
}
