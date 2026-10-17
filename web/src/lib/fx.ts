// Effects: the 8 FX slots, what is loaded in each and where it is used.
// FX 1-4 can be fed from a bus (/fx/N/source, "MIX13") and return on FX
// returns 1-8; any slot can be inserted on a strip, which the console
// reports in /-insert (fx1L … fx8R: the strip index, -1 for none).

import type { Lookup } from "./routing";
import { stripName } from "./routing";
import { stripAt } from "./strips";
import { two } from "./num";

/** FX_NAMES are the effects' full names, by the console's short names. */
export const FX_NAMES: Record<string, string> = {
  HALL: "Hall Reverb",
  AMBI: "Ambience",
  RPLT: "Rich Plate Reverb",
  ROOM: "Room Reverb",
  CHAM: "Chamber Reverb",
  PLAT: "Plate Reverb",
  VREV: "Vintage Reverb",
  VRM: "Vintage Room",
  GATE: "Gated Reverb",
  RVRS: "Reverse Reverb",
  DLY: "Stereo Delay",
  "3TAP": "3-Tap Delay",
  "4TAP": "Rhythm Delay",
  CRS: "Stereo Chorus",
  FLNG: "Stereo Flanger",
  PHAS: "Stereo Phaser",
  DIMC: "Dimensional Chorus",
  FILT: "Mood Filter",
  ROTA: "Rotary Speaker",
  PAN: "Tremolo / Panner",
  SUB: "Sub Octaver",
  "D/RV": "Delay / Chamber",
  "CR/R": "Chorus / Chamber",
  "FL/R": "Flanger / Chamber",
  "D/CR": "Delay / Chorus",
  "D/FL": "Delay / Flanger",
  MODD: "Modulation Delay",
  GEQ2: "Dual Graphic EQ",
  GEQ: "Stereo Graphic EQ",
  TEQ2: "Dual TruEQ",
  TEQ: "Stereo TruEQ",
  DES2: "Dual De-esser",
  DES: "Stereo De-esser",
  P1A: "Stereo Xtec EQ1",
  P1A2: "Dual Xtec EQ1",
  PQ5: "Stereo Xtec EQ5",
  PQ5S: "Dual Xtec EQ5",
  WAVD: "Wave Designer",
  LIM: "Precision Limiter",
  CMB: "Stereo Combinator",
  CMB2: "Dual Combinator",
  FAC: "Stereo Fair Compressor",
  FAC1M: "M/S Fair Compressor",
  FAC2: "Dual Fair Compressor",
  LEC: "Stereo Leisure Compressor",
  LEC2: "Dual Leisure Compressor",
  ULC: "Stereo Ultimo Compressor",
  ULC2: "Dual Ultimo Compressor",
  ENH2: "Dual Enhancer",
  ENH: "Stereo Enhancer",
  EXC2: "Dual Exciter",
  EXC: "Stereo Exciter",
  IMG: "Stereo Imager",
  EDI: "Edison EX1",
  SON: "Sound Maxer",
  AMP2: "Dual Guitar Amp",
  AMP: "Stereo Guitar Amp",
  DRV2: "Dual Tube Stage",
  DRV: "Stereo Tube Stage",
  PIT2: "Dual Pitch Shifter",
  PIT: "Stereo Pitch Shifter",
};

export const fxName = (type: string) => FX_NAMES[type] ?? type;

/** stripOfIndex maps a strip index (0-71, as in /-insert) to its path and label. */
export function stripOfIndex(i: number): { path: string; label: string } | null {
  const s = stripAt(i);
  return s ? { path: s.path, label: s.label } : null;
}

export interface Use {
  /** what uses the slot: "Bus 13 Fx Room", "Mtx 1 Façade" */
  text: string;
  /** the strip to open on the Channel page */
  strip?: string;
  /** L or R side of the slot (inserts), or the input (sources) */
  side: "L" | "R";
}

export interface FxUsage {
  type: string;
  /** FX 1-4 fed from a bus: their sources */
  sources: Use[];
  /** the strips the slot is inserted on */
  inserts: Use[];
  /** FX 1-4: the FX return strips they come back on */
  returns: Use[];
}

const named = (l: Lookup, s: { path: string; label: string }) => {
  const n = stripName(l, s.path);
  return n ? `${s.label} ${n}` : s.label;
};

/** fxUsage tells what is loaded in FX slot 1-8 and where it is used. */
export function fxUsage(l: Lookup, slot: number): FxUsage {
  const type = (l.text(`/fx/${slot}/type`) ?? "").trim();
  const u: FxUsage = { type, sources: [], inserts: [], returns: [] };
  if (slot <= 4) {
    for (const [side, f] of [
      ["L", "l"],
      ["R", "r"],
    ] as const) {
      const src = (l.text(`/fx/${slot}/source/${f}`) ?? "").trim();
      const m = /^MIX(\d+)$/.exec(src);
      if (m) {
        const bus = { path: `/bus/${two(Number(m[1]))}`, label: `Bus ${two(Number(m[1]))}` };
        u.sources.push({ text: named(l, bus), strip: bus.path, side });
      } else if (src === "M/C") {
        u.sources.push({ text: named(l, { path: "/main/m", label: "M/C" }), strip: "/main/m", side });
      }
    }
    if (u.sources.length) {
      for (const [side, n] of [
        ["L", 2 * slot - 1],
        ["R", 2 * slot],
      ] as const) {
        const rtn = { path: `/fxrtn/${two(n)}`, label: `FX ${slot}${side}` };
        u.returns.push({ text: named(l, rtn), strip: rtn.path, side });
      }
    }
  }
  for (const side of ["L", "R"] as const) {
    const s = stripOfIndex(Number(l.value(`/-insert/fx${slot}${side}`) ?? -1));
    if (s) u.inserts.push({ text: named(l, s), strip: s.path, side });
  }
  return u;
}

/**
 * splitDual tells, for the parameter labels of an effect, whether they form
 * two channels ("Gain A", "Gain B"…): the base labels of each, else null.
 */
export function splitDual(labels: string[]): { a: number[]; b: number[] } | null {
  const a: number[] = [];
  const b: number[] = [];
  labels.forEach((label, i) => {
    if (/ A$/.test(label)) a.push(i);
    else if (/ B$/.test(label)) b.push(i);
  });
  if (!a.length || a.length !== b.length || a.length + b.length !== labels.length) return null;
  return { a, b };
}
