// The console's library: 100 scenes, 100 snippets, and 100 presets of each
// kind (channel, effect, routing, AES50/DP48), listed in mirrored nodes
// (/-show/showfile/scene/000…099, /-libs/ch/001…100…) and managed with
// POST /api/library (docs/API.md, "Library").

import { call } from "./api";
import type { Lookup } from "./routing";

export type LibKind = "scene" | "snippet" | "libchan" | "libfx" | "librout" | "libmon";

export interface KindInfo {
  kind: LibKind;
  label: string; // tab label
  one: string; // "scene", "channel preset"
  listing: string;
  /** number of slot 0 in the listing paths: presets count from 1 */
  first: number;
  /** scenes have notes */
  notes?: boolean;
  /** what a preset is saved from and loaded to */
  target?: "strip" | "fx";
  /** snippets cannot be copied */
  noCopy?: boolean;
}

export const KINDS: KindInfo[] = [
  { kind: "scene", label: "Scenes", one: "scene", listing: "/-show/showfile/scene", first: 0, notes: true },
  { kind: "snippet", label: "Snippets", one: "snippet", listing: "/-show/showfile/snippet", first: 0, noCopy: true },
  { kind: "libchan", label: "Channel presets", one: "channel preset", listing: "/-libs/ch", first: 1, target: "strip" },
  { kind: "libfx", label: "Effect presets", one: "effect preset", listing: "/-libs/fx", first: 1, target: "fx" },
  { kind: "librout", label: "Routing presets", one: "routing preset", listing: "/-libs/r", first: 1 },
  { kind: "libmon", label: "AES50 presets", one: "AES50/DP48 preset", listing: "/-libs/mon", first: 1 },
];

export const kindInfo = (k: LibKind) => KINDS.find((i) => i.kind === k)!;

const three = (n: number) => String(n).padStart(3, "0");

/** slotPath is the listing node of a slot (0-99). */
export const slotPath = (k: KindInfo, slot: number) => `${k.listing}/${three(slot + k.first)}`;

/** slotLabel numbers a slot as the console shows it: scenes 00-99, presets 1-100. */
export const slotLabel = (k: KindInfo, slot: number) => (k.first ? String(slot + 1) : String(slot).padStart(2, "0"));

export interface Slot {
  slot: number;
  name: string;
  note: string;
  used: boolean;
}

/** readSlots reads the 100 slots of a kind from the mirror. */
export function readSlots(l: Lookup, k: KindInfo): Slot[] {
  return Array.from({ length: 100 }, (_, slot) => {
    const p = slotPath(k, slot);
    const str = (f: string) => {
      const v = l.value(`${p}/${f}`);
      return typeof v === "string" ? v : "";
    };
    return { slot, name: str("name"), note: k.notes ? str("notes") : "", used: Number(l.value(`${p}/hasdata`) ?? 0) === 1 };
  });
}

/** firstFree is the first unused slot, or -1. */
export const firstFree = (slots: Slot[]) => slots.find((s) => !s.used)?.slot ?? -1;

/** What a channel preset load can bring, as the bits of its scope (0-63). */
export const SCOPE_PARTS = ["Headamp", "Configuration", "Gate", "Compressor", "Equalizer", "Sends"];
export const SCOPE_ALL = 63;

export interface LibraryRequest {
  op: "save" | "load" | "copy" | "delete" | "rename";
  kind: LibKind;
  index: number;
  name?: string;
  note?: string;
  to?: number;
  target?: number;
  scope?: number;
}

export const libraryApi = {
  run: (req: LibraryRequest) => call<void>("POST", "/api/library", req),
};

/** describe says in words what a request does, for confirmations and toasts. */
export function describe(req: LibraryRequest, name: string): string {
  const k = kindInfo(req.kind);
  const what = `${k.one} ${slotLabel(k, req.index)}${name ? ` “${name}”` : ""}`;
  switch (req.op) {
    case "load":
      return k.kind === "scene" || k.kind === "snippet" ? `Recall ${what}` : `Load ${what}`;
    case "save":
      return `Save ${k.one} ${slotLabel(k, req.index)} “${req.name ?? ""}”`;
    case "copy":
      return `Copy ${what} to ${slotLabel(k, req.to ?? 0)}`;
    case "delete":
      return `Delete ${what}`;
    case "rename":
      return `Rename ${what} to “${req.name ?? ""}”`;
  }
}
