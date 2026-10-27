// Meter math: the "meters" events carry linear levels (1.0 = 0 dBFS).

import type { Strip } from "./strips";

export const METER_FLOOR = -60;
/** METER_CLIP is the level a meter is taken to clip from, in dBFS. */
export const METER_CLIP = -0.5;
export const PEAK_HOLD_MS = 1500;
export const PEAK_FALL_DB_PER_S = 20;

/** toDbfs converts a linear level to dBFS. */
export const toDbfs = (level: number) => (level > 0 ? 20 * Math.log10(level) : -Infinity);

/** meterFraction maps dBFS to the meter height: 0 at -60 dB and below, 1 at 0 dBFS and above. */
export const meterFraction = (db: number) => (db <= METER_FLOOR ? 0 : db >= 0 ? 1 : (db - METER_FLOOR) / -METER_FLOOR);

/** DEFAULT_GROUPS are the meter groups the event stream always carries (METER_GROUPS in store/events.ts). */
export const DEFAULT_GROUPS = new Set(["0", "2"]);

/**
 * DCA_GROUP is /meters/5 asked with channels 1-16 and the DCAs: 16 channel
 * meters, then DCA 1-8 (16-23), main L/R and mono. Only these parameters
 * are asked for: the server cannot tell two askings of a group apart.
 */
export const DCA_GROUP = "5:0:0";

/**
 * meterSource tells where a strip's levels are: meter group and indexes.
 * Group 0: 32 inputs, 8 aux, 8 FX returns, 16 buses, 6 matrices; group 2:
 * 16 buses, 6 matrices, main L/R and mono (22-24); DCA_GROUP for the DCAs.
 */
export function meterSource(strip: Strip): { group: string; indexes: number[] } | null {
  const n = Number(strip.path.slice(strip.path.lastIndexOf("/") + 1));
  switch (strip.kind) {
    case "ch":
      return { group: "0", indexes: [n - 1] };
    case "auxin":
      return { group: "0", indexes: [31 + n] };
    case "fxrtn":
      return { group: "0", indexes: [39 + n] };
    case "bus":
      return { group: "0", indexes: [47 + n] };
    case "mtx":
      return { group: "0", indexes: [63 + n] };
    case "main":
      return strip.path === "/main/st" ? { group: "2", indexes: [22, 23] } : { group: "2", indexes: [24] };
    case "dca":
      return { group: DCA_GROUP, indexes: [15 + n] };
  }
  return null;
}

/** GrSource is where a strip's gain reductions are: the gains (1.0 for none) of its gate and dynamics. */
export interface GrSource {
  group: string;
  gate?: number;
  dyn: number;
  /** the strip's level in the same group: its input for a channel, after the fader for the others */
  level: number;
}

/**
 * grSource tells where a strip's gain reductions are. Channels: group 1
 * (32 levels, 32 gate gains, 32 dynamics gains); buses, matrices and mains:
 * group 2, after their levels (16 buses, 6 matrices, L/R, M/C). Aux ins, FX
 * returns and DCAs have no dynamics.
 */
export function grSource(strip: Strip): GrSource | null {
  const n = Number(strip.path.slice(strip.path.lastIndexOf("/") + 1));
  switch (strip.kind) {
    case "ch":
      return { group: "1", gate: 31 + n, dyn: 63 + n, level: n - 1 };
    case "bus":
      return { group: "2", dyn: 24 + n, level: n - 1 };
    case "mtx":
      return { group: "2", dyn: 40 + n, level: 15 + n };
    case "main":
      return strip.path === "/main/st" ? { group: "2", dyn: 47, level: 22 } : { group: "2", dyn: 48, level: 24 };
  }
  return null;
}

/** GR_MAX caps a gain reduction (a gate closed to nothing); GR_SCALE fills a reduction bar. */
export const GR_MAX = 60;
export const GR_SCALE = 30;

/** grDb is the reduction of a gain (1.0 = none) in dB, positive: 0.5 → 6.02. */
export const grDb = (gain: number | undefined) =>
  gain === undefined || !(gain < 1) ? 0 : gain <= 0 ? GR_MAX : Math.min(GR_MAX, -20 * Math.log10(gain));

/** grFraction maps a reduction in dB to a bar's length, full at GR_SCALE. */
export const grFraction = (db: number) => (db <= 0 ? 0 : Math.min(1, db / GR_SCALE));

/** grText shows a reduction: "4.2" (dB), "—" for none worth showing. */
export const grText = (db: number) => (db < 0.05 ? "—" : db >= GR_MAX ? "≥" + GR_MAX : db.toFixed(1));

export interface Peak {
  db: number;
  at: number; // ms
}

/** peakHold updates a held peak with a new level; it returns the peak to show. */
export function peakHold(p: Peak, db: number, now: number): number {
  const shown = p.db - (Math.max(0, now - p.at - PEAK_HOLD_MS) / 1000) * PEAK_FALL_DB_PER_S;
  if (db >= shown || Number.isNaN(shown)) {
    p.db = db;
    p.at = now;
    return db;
  }
  return shown;
}
