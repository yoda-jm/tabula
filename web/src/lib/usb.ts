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

export type UsbOp = "play" | "pause" | "stop" | "prev" | "next";

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
      return { label: "Load…", asks: true };
  }
  return null; // shows, preferences, other files: not from here
}

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

/** recordsToStick tells whether the console records to its stick (/-stat/tape/state RECORD, or paused): Stop asks first. */
export const recordsToStick = (state: string) => state === "RECORD" || state === "RPAUSE";

/** isPlaying tells whether the player's file is this entry of the directory shown. */
export function isPlaying(file: string, path: string, name: string): boolean {
  const parts = file.split("/").filter(Boolean);
  if (!parts.length || parts[parts.length - 1] !== name) return false;
  return (parts[parts.length - 2] ?? "") === path;
}
