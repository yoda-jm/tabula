// The RTA (/meters/15): 100 bands in dB, ten per octave from 19.5 Hz to
// 18.7 kHz, of the source chosen with /-action/setrtasrc and reported in
// /-stat/rtasource (docs/emulator.md, "RTA"): 0-31 channels, 32-39 aux
// inputs, 40-47 FX returns, 48-63 buses, 64-69 matrices, 70 L/R, 71 M/C,
// 72 monitor, 73 talkback, 74-89 dual graphic EQ A/B and 90-97 stereo
// graphic EQ of FX slots 1-8, before the EQ; 98 more for after it.

import { stripAt, stripIndex } from "./strips";

export const RTA_BANDS = 100;
export const RTA_POST = 98;

/** rtaFreq is the centre frequency of RTA band i. */
export const rtaFreq = (i: number) => 1250 * Math.pow(2, (i - 60) / 10);

/** rtaSourceLabel names an RTA source: "Ch 12 (pre EQ)", "FX 5 GEQ A (post EQ)". */
export function rtaSourceLabel(n: number): string {
  if (!Number.isInteger(n) || n < 0 || n >= 2 * RTA_POST) return String(n);
  const post = n >= RTA_POST;
  const s = n % RTA_POST;
  let label: string;
  if (s < 72) label = stripAt(s)!.label;
  else if (s === 72) label = "Monitor";
  else if (s === 73) label = "Talkback";
  else if (s < 90) label = `FX ${Math.floor((s - 74) / 2) + 1} GEQ ${(s - 74) % 2 ? "B" : "A"}`;
  else label = `FX ${s - 89} GEQ`;
  return label + (post ? " (post EQ)" : " (pre EQ)");
}

/** Graphic EQ effects: dual ones have two sides of 31 bands and a master. */
export const GEQ_TYPES = ["GEQ", "GEQ2", "TEQ", "TEQ2"];
export const isGeq = (type: string | undefined) => !!type && GEQ_TYPES.includes(type);
export const isDualGeq = (type: string | undefined) => type === "GEQ2" || type === "TEQ2";

/** geqRtaSource is the RTA source of a graphic EQ in FX slot 1-8 (side 0 = A, 1 = B). */
export function geqRtaSource(slot: number, type: string, side: 0 | 1, post: boolean): number {
  const base = isDualGeq(type) ? 74 + 2 * (slot - 1) + side : 89 + slot;
  return base + (post ? RTA_POST : 0);
}

/**
 * rtaWrite tells whether a graphic EQ editor writes its RTA source now: once
 * for each source it wants (when it opens, or its side or tap changes), when
 * it may write, and not when the RTA listens there already. Never again
 * because the source moved away (the console's surface or another tab chose
 * another): that would ping-pong. handled is the source it dealt with last
 * (-1 at first); the answer gives the next one.
 */
export function rtaWrite(handled: number, want: number, current: number, can: boolean): { handled: number; write: boolean } {
  if (!can || handled === want) return { handled, write: false };
  return { handled: want, write: current !== want };
}

/** stripRtaSource is the RTA source number of a strip (before its EQ): its strip index, or null. */
export function stripRtaSource(path: string): number | null {
  const i = stripIndex(path);
  return i < 0 ? null : i;
}

/** geqBandFreq is the centre of graphic EQ band i (0-30): third octaves, 1 kHz is band 17. */
export const geqBandFreq = (i: number) => 1000 * Math.pow(2, (i - 17) / 3);

/** GEQ_LABELS are the band names the console prints. */
// prettier-ignore
export const GEQ_LABELS = [
  "20", "25", "31.5", "40", "50", "63", "80", "100", "125", "160", "200", "250", "315", "400", "500", "630",
  "800", "1k", "1k25", "1k6", "2k", "2k5", "3k15", "4k", "5k", "6k3", "8k", "10k", "12k5", "16k", "20k",
];

/**
 * geqPosition places a frequency on the axis of the 31 graphic EQ bands,
 * in band units (band i is centred on i + 0.5): the RTA is drawn behind the
 * faders on the same axis.
 */
export const geqPosition = (f: number) => 17 + 3 * Math.log2(f / 1000) + 0.5;

/**
 * rtaWindow is the dB range the RTA is drawn over, from /-prefs/rta as
 * M32-Edit reads it: a 30 dB window under -gain, or 60 dB under -gain30
 * when range is ON.
 */
export function rtaWindow(gain: number, wide: boolean, gain30: number): { top: number; bottom: number } {
  const g = wide ? gain30 : gain;
  const range = wide ? 60 : 30;
  return { top: -g, bottom: -g - range };
}

/** rtaFraction maps a dB value into the window: 0 at the bottom, 1 at the top. */
export function rtaFraction(db: number, w: { top: number; bottom: number }): number {
  if (!Number.isFinite(db) || db <= w.bottom) return 0;
  if (db >= w.top) return 1;
  return (db - w.bottom) / (w.top - w.bottom);
}
