// Meter math: the "meters" events carry linear levels (1.0 = 0 dBFS).

import type { Strip } from "./strips";

export const METER_FLOOR = -60;
export const PEAK_HOLD_MS = 1500;
export const PEAK_FALL_DB_PER_S = 20;

/** toDbfs converts a linear level to dBFS. */
export const toDbfs = (level: number) => (level > 0 ? 20 * Math.log10(level) : -Infinity);

/** meterFraction maps dBFS to the meter height: 0 at -60 dB and below, 1 at 0 dBFS and above. */
export const meterFraction = (db: number) => (db <= METER_FLOOR ? 0 : db >= 0 ? 1 : (db - METER_FLOOR) / -METER_FLOOR);

/**
 * meterSource tells where a strip's levels are: meter group and indexes.
 * Group 0: 32 inputs, 8 aux, 8 FX returns, 16 buses, 6 matrices; group 2:
 * 16 buses, 6 matrices, main L/R and mono (22-24). DCAs have no meter.
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
  }
  return null;
}

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
