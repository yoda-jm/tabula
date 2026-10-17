// Mixer strips: which console paths make the mixer banks, and which of
// their parameters exist (read from the schema, not assumed).

import type { Schema } from "./schema";
import { range, two } from "./num";

export type StripKind = "ch" | "auxin" | "fxrtn" | "bus" | "mtx" | "main" | "dca";

export interface Strip {
  path: string; // /ch/01, /main/st, /dca/1
  kind: StripKind;
  label: string; // "Ch 01", "LR", "DCA 1"
}

const ch = (n: number): Strip => ({ path: `/ch/${two(n)}`, kind: "ch", label: `Ch ${two(n)}` });
const auxin = (n: number): Strip => ({ path: `/auxin/${two(n)}`, kind: "auxin", label: `Aux ${n}` });
const fxrtn = (n: number): Strip => ({
  path: `/fxrtn/${two(n)}`,
  kind: "fxrtn",
  label: `FX ${Math.ceil(n / 2)}${n % 2 ? "L" : "R"}`,
});
const bus = (n: number): Strip => ({ path: `/bus/${two(n)}`, kind: "bus", label: `Bus ${two(n)}` });
const mtx = (n: number): Strip => ({ path: `/mtx/${two(n)}`, kind: "mtx", label: `Mtx ${n}` });
const dca = (n: number): Strip => ({ path: `/dca/${n}`, kind: "dca", label: `DCA ${n}` });

export interface Bank {
  id: string;
  label: string;
  strips: Strip[];
}

export const BANKS: Bank[] = [
  { id: "in1", label: "Inputs 1-16", strips: range(1, 16).map(ch) },
  { id: "in2", label: "Inputs 17-32", strips: range(17, 32).map(ch) },
  { id: "aux", label: "Aux & FX returns", strips: [...range(1, 8).map(auxin), ...range(1, 8).map(fxrtn)] },
  { id: "bus", label: "Buses 1-16", strips: range(1, 16).map(bus) },
  {
    id: "main",
    label: "Matrices & Main",
    strips: [
      ...range(1, 6).map(mtx),
      { path: "/main/st", kind: "main", label: "LR" },
      { path: "/main/m", kind: "main", label: "M/C" },
    ],
  },
  { id: "dca", label: "DCAs", strips: range(1, 8).map(dca) },
];

export const ALL_STRIPS: Strip[] = BANKS.flatMap((b) => b.strips);

export const stripByPath = (path: string) => ALL_STRIPS.find((s) => s.path === path);

/**
 * INDEXED_STRIPS are the strips in the console's strip numbering, 0-71:
 * channels, aux ins, FX returns, buses, matrices, LR, M/C. /-insert, channel
 * presets and the RTA sources count them so.
 */
export const INDEXED_STRIPS = ALL_STRIPS.slice(0, 72);

export const stripAt = (i: number): Strip | undefined => (Number.isInteger(i) && i >= 0 ? INDEXED_STRIPS[i] : undefined);

export const stripIndex = (path: string) => INDEXED_STRIPS.findIndex((s) => s.path === path);

/** Addresses a mixer strip shows; a field is absent when the schema lacks it. */
export interface StripAddrs {
  name?: string;
  color?: string;
  fader?: string;
  on?: string;
  pan?: string;
}

export function stripAddrs(schema: Schema, strip: Strip): StripAddrs {
  const has = (addr: string) => (schema.leaves.has(addr) ? addr : undefined);
  const p = strip.path;
  return {
    name: has(`${p}/config/name`),
    color: has(`${p}/config/color`),
    fader: has(`${p}/mix/fader`) ?? has(`${p}/fader`),
    on: has(`${p}/mix/on`) ?? has(`${p}/on`),
    pan: has(`${p}/mix/pan`),
  };
}

/** Where a strip's sends (mix/NN) go: inputs feed the buses, buses and mains the matrices. */
export function sendTarget(strip: Strip, n: number): { path: string; label: string } {
  if (strip.kind === "bus" || strip.kind === "main") return { path: `/mtx/${two(n)}`, label: `Mtx ${n}` };
  return { path: `/bus/${two(n)}`, label: `Bus ${two(n)}` };
}

export interface Send {
  path: string; // /ch/01/mix/03
  n: number;
  target: { path: string; label: string };
}

export function sendsOf(schema: Schema, strip: Strip): Send[] {
  const out: Send[] = [];
  for (let n = 1; n <= 16; n++) {
    const path = `${strip.path}/mix/${two(n)}`;
    if (schema.byPath.has(path)) out.push({ path, n, target: sendTarget(strip, n) });
  }
  return out;
}

/** eqBandsOf returns the EQ band node paths of a strip (eq/1…eq/6). */
export function eqBandsOf(schema: Schema, strip: Strip): string[] {
  const out: string[] = [];
  for (let n = 1; n <= 6; n++) {
    const path = `${strip.path}/eq/${n}`;
    if (schema.byPath.has(path)) out.push(path);
  }
  return out;
}

export type SectionId =
  | "config"
  | "preamp"
  | "delay"
  | "gate"
  | "dyn"
  | "insert"
  | "eq"
  | "main"
  | "sends"
  | "groups"
  | "automix";

/** sectionsOf lists the Channel page sections the strip has, in console order. */
export function sectionsOf(schema: Schema, strip: Strip): SectionId[] {
  const has = (sub: string) => schema.byPath.has(sub ? `${strip.path}/${sub}` : strip.path);
  const out: SectionId[] = [];
  if (has("config")) out.push("config");
  if (has("preamp")) out.push("preamp");
  if (has("delay")) out.push("delay");
  if (has("gate")) out.push("gate");
  if (has("dyn")) out.push("dyn");
  if (has("insert")) out.push("insert");
  if (has("eq")) out.push("eq");
  if (has("mix") || (strip.kind === "dca" && has(""))) out.push("main");
  if (sendsOf(schema, strip).length) out.push("sends");
  if (has("grp")) out.push("groups");
  if (has("automix")) out.push("automix");
  return out;
}

/** Scribble strip colours, by enum index (the "i" variants are inverted). */
export const COLOURS = ["OFF", "RD", "GN", "YE", "BL", "MG", "CY", "WH"] as const;

export function colourOf(index: number | undefined): { name: string; inverted: boolean } {
  const i = Number(index ?? 0);
  return { name: COLOURS[i % 8] ?? "OFF", inverted: i >= 8 };
}

/**
 * sourceLabel names an input source number (/ch/NN/config/source,
 * /auxin/NN/config/source) as the console lists them.
 */
export function sourceLabel(n: number): string {
  if (n <= 0) return "OFF";
  if (n <= 32) return `In ${two(n)}`;
  if (n <= 38) return `Aux ${n - 32}`;
  if (n === 39) return "USB L";
  if (n === 40) return "USB R";
  if (n <= 48) return `FX ${Math.ceil((n - 40) / 2)}${n % 2 ? "L" : "R"}`;
  if (n <= 64) return `Bus ${two(n - 48)}`;
  return String(n);
}

// --- linked pairs ---

const LINK_FLAGS: Partial<Record<StripKind, string>> = {
  ch: "/config/chlink",
  auxin: "/config/auxlink",
  fxrtn: "/config/fxlink",
  bus: "/config/buslink",
  mtx: "/config/mtxlink",
};

export interface Link {
  /** the link switch of the pair, e.g. /config/chlink/1-2 */
  flag: string;
  odd: string;
  even: string;
}

/** linkOf tells which pair a strip belongs to (channels, aux ins, FX returns, buses, matrices). */
export function linkOf(strip: Strip): Link | null {
  const group = LINK_FLAGS[strip.kind];
  if (!group) return null;
  const n = Number(strip.path.slice(strip.path.lastIndexOf("/") + 1));
  const odd = n % 2 ? n : n - 1;
  const base = strip.path.slice(0, strip.path.lastIndexOf("/") + 1);
  return { flag: `${group}/${odd}-${odd + 1}`, odd: base + two(odd), even: base + two(odd + 1) };
}

/** pairLabel names a linked pair: "Ch 01-02", "Aux 7-8", "FX 1", "Bus 07-08", "Mtx 1-2". */
export function pairLabel(odd: Strip): string {
  const n = Number(odd.path.slice(odd.path.lastIndexOf("/") + 1));
  switch (odd.kind) {
    case "ch":
      return `Ch ${two(n)}-${two(n + 1)}`;
    case "auxin":
      return `Aux ${n}-${n + 1}`;
    case "fxrtn":
      return `FX ${Math.ceil(n / 2)}`;
    case "bus":
      return `Bus ${two(n)}-${two(n + 1)}`;
    case "mtx":
      return `Mtx ${n}-${n + 1}`;
  }
  return odd.label;
}

/** A mixer strip: one strip, or a linked pair shown as one stereo strip. */
export interface MixerStrip {
  strip: Strip; // the odd strip of a pair: its controls drive both
  partner?: Strip;
  label: string;
}

/**
 * pairStrips merges each linked pair of a bank into one stereo strip.
 * linked tells whether a pair's link switch is on and its faders and mutes
 * follow each other (/config/linkcfg/fdrmute): the console then mirrors the
 * odd strip's fader and mute to the even one.
 */
export function pairStrips(strips: Strip[], linked: (flag: string) => boolean): MixerStrip[] {
  const out: MixerStrip[] = [];
  for (let i = 0; i < strips.length; i++) {
    const s = strips[i];
    const l = linkOf(s);
    const next = strips[i + 1];
    if (l && l.odd === s.path && next?.path === l.even && linked(l.flag)) {
      out.push({ strip: s, partner: next, label: pairLabel(s) });
      i++;
    } else {
      out.push({ strip: s, label: s.label });
    }
  }
  return out;
}
