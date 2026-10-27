// Scene safes and snippet filters: what a scene recall leaves alone, and
// what a snippet holds and changes. Bitmaps of the console's library nodes,
// bits as in Maillot's document (docs/gui.md, "Scene safes and snippet
// filters"): the safes of one scene (/-show/showfile/scene/NNN/safes), those
// of every recall (/-show/showfile/show: parameter and channel safes) and
// a snippet's filters (/-show/showfile/snippet/NNN/eventtyp, channels,
// auxbuses, maingrps).

import { three } from "./num";
import type { Lookup } from "./routing";

/** One bit of a bitmap parameter: what it stands for, and the strip it selects. */
export interface Bit {
  bit: number;
  label: string;
  path?: string;
}

/** A row of bits of one parameter. */
export interface BitRow {
  addr: string;
  label: string;
  bits: Bit[];
}

export const SHOW = "/-show/showfile/show";
export const MAIN_LEVELS = "/-prefs/safe_masterlevels";

const named = (labels: string[], first = 0): Bit[] => labels.map((label, i) => ({ bit: first + i, label }));
const range = (n: number, first: number, label: (i: number) => string, path?: (i: number) => string): Bit[] =>
  Array.from({ length: n }, (_, i) => ({ bit: first + i, label: label(i + 1), path: path?.(i + 1) }));
const two = (i: number) => String(i).padStart(2, "0");

/** sceneSafesAddr is the safes of one scene (bits 1-8); scene 00 has none. */
export const sceneSafesAddr = (slot: number) => `/-show/showfile/scene/${three(slot)}/safes`;

export const SCENE_SAFES: Bit[] = named(
  ["Talkback", "Effects", "Mix buses", "Channels", "Configuration", "Preamps", "Output patch", "Routing"],
  1,
);

/** What the parameter safes of a strip keep, bits 0-7 (buses: "Sends" to the matrices, no preamp). */
const STRIP_PARTS = ["Preamp", "Config", "EQ", "Gate & comp", "Insert", "Groups", "Fader & pan", "Mute"];

/** PARAM_SAFES are the parameter safes of every scene recall (the console's PARAM SAFE page). */
export const PARAM_SAFES: BitRow[] = [
  { addr: `${SHOW}/inputs`, label: "Input channels", bits: named(STRIP_PARTS) },
  { addr: `${SHOW}/mxsends`, label: "Sends of the inputs to", bits: range(16, 0, (i) => `Mix ${i}`) },
  { addr: `${SHOW}/mxbuses`, label: "Mix buses", bits: named(["Sends", ...STRIP_PARTS.slice(1, 3), "Comp", ...STRIP_PARTS.slice(4)]) },
  {
    addr: `${SHOW}/console`,
    label: "Console",
    bits: named(["Configuration", "Solo", "Routing", "Output patch", "User in", "User out", "Surface"]),
  },
];

/** CHANNEL_SAFES are the strips every scene recall keeps whole (the console's CHAN SAFE page). */
export const CHANNEL_SAFES: BitRow[] = [
  { addr: `${SHOW}/chan16`, label: "Channels 1-16", bits: range(16, 0, String, (i) => `/ch/${two(i)}`) },
  { addr: `${SHOW}/chan32`, label: "Channels 17-32", bits: range(16, 0, (i) => String(i + 16), (i) => `/ch/${two(i + 16)}`) },
  {
    addr: `${SHOW}/return`,
    label: "Aux and FX returns",
    bits: [
      ...range(8, 0, (i) => `Aux ${i}`, (i) => `/auxin/${two(i)}`),
      ...range(8, 8, (i) => `FX ${Math.ceil(i / 2)}${i % 2 ? "L" : "R"}`, (i) => `/fxrtn/${two(i)}`),
    ],
  },
  { addr: `${SHOW}/buses`, label: "Mix buses", bits: range(16, 0, (i) => `Mix ${i}`, (i) => `/bus/${two(i)}`) },
  {
    addr: `${SHOW}/lrmtxdca`,
    label: "Matrices, main, DCAs",
    bits: [
      ...range(6, 0, (i) => `Mtx ${i}`, (i) => `/mtx/${two(i)}`),
      { bit: 6, label: "L/R", path: "/main/st" },
      { bit: 7, label: "M/C", path: "/main/m" },
      ...range(8, 8, (i) => `DCA ${i}`, (i) => `/dca/${i}`),
    ],
  },
  { addr: `${SHOW}/effects`, label: "Effects", bits: range(8, 0, (i) => `FX ${i}`) },
];

/** snippetRows are a snippet's filters: what it holds (parameters, effects, console) and of which strips. */
export function snippetRows(slot: number): { params: BitRow[]; strips: BitRow[] } {
  const p = `/-show/showfile/snippet/${three(slot)}`;
  const ev = `${p}/eventtyp`;
  return {
    params: [
      {
        addr: ev,
        label: "Parameters",
        bits: named([...STRIP_PARTS, "Sends 1-8", "Sends 9-12", "Sends 13-16", "L/R & M/C", "Matrix sends"]),
      },
      { addr: ev, label: "Effects", bits: range(8, 13, (i) => `FX ${i}`) },
      { addr: ev, label: "Console", bits: named(["Configuration", "Solo", "Routing", "Output patch"], 21) },
    ],
    strips: [
      { addr: `${p}/channels`, label: "Channels", bits: range(32, 0, String, (i) => `/ch/${two(i)}`) },
      {
        addr: `${p}/auxbuses`,
        label: "Aux, FX returns, buses",
        bits: [
          ...range(8, 0, (i) => `Aux ${i}`, (i) => `/auxin/${two(i)}`),
          ...range(8, 8, (i) => `FX ${Math.ceil(i / 2)}${i % 2 ? "L" : "R"}`, (i) => `/fxrtn/${two(i)}`),
          ...range(16, 16, (i) => `Mix ${i}`, (i) => `/bus/${two(i)}`),
        ],
      },
      {
        addr: `${p}/maingrps`,
        label: "Matrices, main, DCAs",
        bits: [
          ...range(6, 0, (i) => `Mtx ${i}`, (i) => `/mtx/${two(i)}`),
          { bit: 6, label: "L/R", path: "/main/st" },
          { bit: 7, label: "M/C", path: "/main/m" },
          ...range(8, 8, (i) => `DCA ${i}`, (i) => `/dca/${i}`),
        ],
      },
    ],
  };
}

/** isSet tells a bit of a 32-bit map (as the console sends it: bit 31 makes it negative). */
export const isSet = (v: number, bit: number) => ((v >>> bit) & 1) === 1;

/** bitsOf reads a bitmap parameter, 0 when it is not known. */
export function bitsOf(l: Lookup, addr: string): number {
  const v = Number(l.value(addr) ?? 0);
  return Number.isFinite(v) ? v : 0;
}

/** ticked lists the labels of a row's bits that are set. */
export const ticked = (v: number, bits: Bit[]) => bits.filter((b) => isSet(v, b.bit)).map((b) => b.label);

/**
 * compact shortens a list of labels sharing a prefix and numbered in a row:
 * ["Mix 1", "Mix 2", "Mix 3", "Mix 5"] → "Mix 1-3, 5"; "1", "2" → "1-2".
 */
export function compact(labels: string[]): string {
  const out: string[] = [];
  let prefix = "";
  let run: number[] = [];
  const flush = () => {
    if (!run.length) return;
    const parts: string[] = [];
    for (let i = 0; i < run.length; ) {
      let j = i;
      while (j + 1 < run.length && run[j + 1] === run[j] + 1) j++;
      parts.push(j > i ? `${run[i]}-${run[j]}` : String(run[i]));
      i = j + 1;
    }
    out.push(prefix + parts.join(", "));
    run = [];
  };
  for (const label of labels) {
    const m = /^(.*?)(\d+)$/.exec(label);
    if (m && (m[1] === prefix || !run.length)) {
      prefix = m[1];
      run.push(Number(m[2]));
      continue;
    }
    flush();
    if (m) {
      prefix = m[1];
      run.push(Number(m[2]));
    } else out.push(label);
  }
  flush();
  return out.join(", ");
}

/** sceneKeeps says what a recall of a scene leaves alone, one line per kind of safe; none when it changes everything. */
export function sceneKeeps(l: Lookup, slot: number): string[] {
  const out: string[] = [];
  if (slot > 0) {
    const own = ticked(bitsOf(l, sceneSafesAddr(slot)), SCENE_SAFES);
    if (own.length) out.push(`This scene's safes: ${own.join(", ")}`);
  }
  for (const r of PARAM_SAFES) {
    const t = ticked(bitsOf(l, r.addr), r.bits);
    if (t.length) out.push(`${r.label}: ${compact(t)}`);
  }
  const channels = (r: BitRow) => r.addr.endsWith("chan16") || r.addr.endsWith("chan32");
  const strips = CHANNEL_SAFES.flatMap((r) => ticked(bitsOf(l, r.addr), r.bits).map((t) => (channels(r) ? `Ch ${t}` : t)));
  if (strips.length) out.push(`Whole strips: ${compact(strips)}`);
  if (Number(l.value(MAIN_LEVELS) ?? 0) === 1) out.push("Main levels (L/R and M/C faders)");
  return out;
}

/** snippetChanges says what a recall of a snippet changes, empty when it changes nothing. */
export function snippetChanges(l: Lookup, slot: number): string {
  const { params, strips } = snippetRows(slot);
  const [kinds, fx, consoleParts] = params.map((r) => ticked(bitsOf(l, r.addr), r.bits));
  const sel = strips.flatMap((r) => ticked(bitsOf(l, r.addr), r.bits).map((t) => (r.addr.endsWith("channels") ? `Ch ${t}` : t)));
  const out: string[] = [];
  if (kinds.length && sel.length) out.push(`${kinds.join(", ")} of ${compact(sel)}`);
  if (fx.length) out.push(compact(fx));
  if (consoleParts.length) out.push(`the console's ${consoleParts.join(", ").toLowerCase()}`);
  return out.join("; ");
}
