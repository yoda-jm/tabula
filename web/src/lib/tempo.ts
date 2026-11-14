// Tap tempo for the effects: tapped beats give a tempo, which sets the
// delay times (in ms) and the modulation speeds (in Hz) of an FX slot to a
// note value of it. The console's own tap (a user button on an effect's
// Time) takes the last interval alone; here the last few are averaged, and a
// pause or a tap far off the beat starts a new tempo.

/** Taps further apart than this start over: the delays reach 3000 ms. */
export const MAX_GAP = 3000;
/** Intervals averaged: the last 4 (5 taps). */
export const MAX_INTERVALS = 4;
/** A tap this far off the average (±40%) starts a new tempo from the last tap. */
export const OFF_BEAT = 0.4;
export const MIN_BPM = 20;
export const MAX_BPM = 300;

/**
 * addTap adds a tap (ms, from performance.now) to the taps of the tempo
 * being tapped: the taps kept, the oldest first.
 */
export function addTap(taps: number[], now: number): number[] {
  const last = taps[taps.length - 1];
  if (last === undefined || now - last > MAX_GAP || now <= last) return [now];
  const avg = beatMs(taps);
  if (avg !== null && Math.abs(now - last - avg) > OFF_BEAT * avg) return [last, now];
  return [...taps, now].slice(-(MAX_INTERVALS + 1));
}

/** beatMs is the beat of the taps: the average of their intervals, null under two taps. */
export function beatMs(taps: number[]): number | null {
  if (taps.length < 2) return null;
  return (taps[taps.length - 1] - taps[0]) / (taps.length - 1);
}

export const bpmOf = (ms: number) => 60000 / ms;
export const msOf = (bpm: number) => 60000 / bpm;

/** formatBpm shows a tempo with one decimal, 120 rather than 120.0. */
export const formatBpm = (bpm: number) => String(Math.round(bpm * 10) / 10);

/** Note values, in beats (quarter notes): dotted ones are 3/2, triplets 2/3. */
export const NOTES: { name: string; beats: number }[] = [
  { name: "1/1", beats: 4 },
  { name: "1/2", beats: 2 },
  { name: "1/4.", beats: 1.5 },
  { name: "1/4", beats: 1 },
  { name: "1/4T", beats: 2 / 3 },
  { name: "1/8.", beats: 0.75 },
  { name: "1/8", beats: 0.5 },
  { name: "1/8T", beats: 1 / 3 },
  { name: "1/16", beats: 0.25 },
];

export const noteBeats = (name: string) => NOTES.find((n) => n.name === name)?.beats ?? 1;

export type TargetKind = "time" | "rate";

/** A Target is a parameter the tempo sets: a delay time, or an LFO speed. */
export interface Target {
  /** the parameter's field name, "02" */
  field: string;
  label: string;
  kind: TargetKind;
  /** set from the tempo unless unticked: delay times, or the speed of an effect without one */
  on: boolean;
}

// The effects with a delay time, and those with an LFO speed in Hz (the
// rotary speaker's speeds and the envelope speeds are not musical).
const TIMED = new Set(["DLY", "3TAP", "4TAP", "D/RV", "D/CR", "D/FL", "MODD"]);
const MODULATED = new Set(["CRS", "FLNG", "PHAS", "FILT", "PAN", "D/CR", "D/FL", "CR/R", "FL/R", "MODD"]);

/** hasTempo tells whether an effect has parameters a tempo sets. */
export const hasTempo = (type: string) => TIMED.has(type) || MODULATED.has(type);

/**
 * tempoTargets finds, in the parameters of an effect (field names and
 * labels), the ones a tempo sets: "Time", and "Speed" or "Rate".
 */
export function tempoTargets(type: string, fields: { name: string; label?: string }[]): Target[] {
  const out: Target[] = [];
  for (const f of fields) {
    if (TIMED.has(type) && f.label === "Time") out.push({ field: f.name, label: f.label, kind: "time", on: true });
    if (MODULATED.has(type) && (f.label === "Speed" || f.label === "Rate")) {
      out.push({ field: f.name, label: f.label, kind: "rate", on: false });
    }
  }
  if (!out.some((t) => t.on)) for (const t of out) t.on = true;
  return out;
}

/** targetValue is what a tempo sets a target to, for a note value: ms for a time, Hz for a speed. */
export function targetValue(kind: TargetKind, bpm: number, beats: number): number {
  return kind === "time" ? msOf(bpm) * beats : bpm / 60 / beats;
}

/** tempoOfValue is the tempo a time or a speed stands for, for a note value. */
export function tempoOfValue(kind: TargetKind, value: number, beats: number): number {
  return kind === "time" ? bpmOf(value / beats) : value * 60 * beats;
}

/** parseBpm reads a typed tempo, "120" or "92.5", NaN when it is not one. */
export function parseBpm(text: string): number {
  const t = text.trim().replace(",", ".");
  if (!/^\d+(\.\d+)?$/.test(t)) return NaN;
  const bpm = Number(t);
  return bpm >= MIN_BPM && bpm <= MAX_BPM ? bpm : NaN;
}

/**
 * isTapKey tells whether a key press taps the tempo: T alone, not held down
 * (the FX page; a field being typed in keeps its T).
 */
export function isTapKey(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; repeat: boolean }): boolean {
  return e.key.toLowerCase() === "t" && !e.ctrlKey && !e.metaKey && !e.altKey && !e.repeat;
}
