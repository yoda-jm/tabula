// Value math of the console's parameters (docs/API.md, "Schema"): floats are
// normalized to [0, 1] on the wire and quantized to a number of known steps;
// the displayed value goes through a linear, logarithmic or fader-law scale.
// Pure functions only: everything here is unit tested.

import type { RawValue, TypeInfo } from "./api";

export const isFloatKind = (t: TypeInfo) => t.kind === "lin" || t.kind === "log" || t.kind === "level";

export const clamp01 = (v: number) => (Number.isNaN(v) || v <= 0 ? 0 : v >= 1 ? 1 : v);

/** quantize snaps a normalized value to one of `steps` known values (0 = continuous). */
export function quantize(v: number, steps = 0): number {
  const c = clamp01(v);
  if (!steps || steps < 2) return c;
  const last = steps - 1;
  return Math.round(c * last) / last;
}

/** stepSize is the normalized distance between two known values. */
export const stepSize = (steps = 0) => (steps > 1 ? 1 / (steps - 1) : 0);

// The fader law: four linear segments meeting at -60, -30 and -10 dB; 0.75
// is 0 dB, 1 is +10 dB, 0 is -oo.
export function levelToDb(v: number): number {
  if (v <= 0) return -Infinity;
  if (v < 0.0625) return 480 * v - 90;
  if (v < 0.25) return 160 * v - 70;
  if (v < 0.5) return 80 * v - 50;
  return 40 * v - 30;
}

export function dbToLevel(db: number): number {
  if (db === -Infinity || db < -90) return 0;
  if (db < -60) return (db + 90) / 480;
  if (db < -30) return (db + 70) / 160;
  if (db < -10) return (db + 50) / 80;
  return clamp01((db + 30) / 40);
}

export const linToValue = (v: number, min: number, max: number) => min + v * (max - min);
export const valueToLin = (x: number, min: number, max: number) => (x - min) / (max - min);
export const logToValue = (v: number, min: number, max: number) => min * Math.exp(v * Math.log(max / min));
export const valueToLog = (x: number, min: number, max: number) => Math.log(x / min) / Math.log(max / min);

/** toDisplay turns a normalized float into the displayed value (dB, Hz, ms…). */
export function toDisplay(t: TypeInfo, norm: number): number {
  const v = quantize(norm, t.steps);
  switch (t.kind) {
    case "level":
      return levelToDb(v);
    case "lin":
      return linToValue(v, t.min ?? 0, t.max ?? 1);
    case "log":
      return logToValue(v, t.min ?? 1, t.max ?? 10);
  }
  return v;
}

/** fromDisplay is the inverse of toDisplay, quantized. */
export function fromDisplay(t: TypeInfo, x: number): number {
  let v: number;
  switch (t.kind) {
    case "level":
      v = dbToLevel(x);
      break;
    case "lin":
      v = valueToLin(x, t.min ?? 0, t.max ?? 1);
      break;
    case "log":
      v = valueToLog(x, t.min ?? 1, t.max ?? 10);
      break;
    default:
      v = x;
  }
  return quantize(v, t.steps);
}

/** round rounds to prec decimals, ties away from zero, never a negative zero (like the console). */
export function round(v: number, prec: number): number {
  const p = Math.pow(10, prec);
  const r = (Math.sign(v) * Math.round(Math.abs(v) * p)) / p;
  return r === 0 ? 0 : r;
}

const fixed = (v: number, prec: number) => round(v, prec).toFixed(prec);

/** formatDb prints a dB level like the console: "-oo", "-12.0", "0.0", "+0.3". */
export function formatDb(db: number): string {
  if (db === -Infinity || Number.isNaN(db)) return "-oo";
  const r = round(db, 1);
  return (r > 0 ? "+" : "") + fixed(r, 1);
}

/** formatHz prints a frequency like the console: 295.8 below 1 kHz, 1k49 (truncated) above. */
export function formatHz(f: number): string {
  if (f < 1000) return fixed(f, 1);
  const k = Math.floor(f / 1000);
  const rest = Math.trunc((f - k * 1000) / 10);
  return `${k}k${String(rest).padStart(2, "0")}`;
}

/** decimals is how many decimals make the step size of a linear type readable. */
export function decimals(t: TypeInfo): number {
  const steps = t.steps ?? 0;
  const width = Math.abs((t.max ?? 1) - (t.min ?? 0));
  if (steps < 2) return width >= 50 ? 0 : width > 2 ? 1 : 2;
  const step = width / (steps - 1);
  if (step >= 1 - 1e-9) return 0;
  for (let d = 1; d <= 3; d++) {
    const s = step * Math.pow(10, d);
    if (Math.abs(s - Math.round(s)) < 1e-6) return d;
  }
  return 3;
}

/** logText prints log-scale values roughly like the console: three significant digits. */
function logText(v: number, unit?: string): string {
  const a = Math.abs(v);
  if (a >= 100) return fixed(v, 0);
  if (a >= 10) return fixed(v, 1);
  return fixed(v, unit ? 2 : 1);
}

/**
 * formatNorm prints a normalized float the way the console would (close
 * enough while a control is being dragged; the console's own text replaces
 * it as soon as the change comes back).
 */
export function formatNorm(t: TypeInfo, norm: number): string {
  const x = toDisplay(t, norm);
  switch (t.kind) {
    case "level":
      return formatDb(x);
    case "lin": {
      const s = fixed(x, decimals(t));
      return (t.min ?? 0) < 0 && round(x, decimals(t)) >= 0 ? "+" + s : s;
    }
    case "log":
      return t.unit === "Hz" ? formatHz(x) : logText(x, t.unit);
  }
  return String(x);
}

/** formatValue prints any raw value of a type (enum names, bitmaps as %0101…). */
export function formatValue(t: TypeInfo, value: RawValue | undefined): string {
  if (value === undefined) return "";
  switch (t.kind) {
    case "enum":
      return t.names?.[Number(value)] ?? String(value);
    case "bitmap":
      return "%" + toBits(Number(value), t.bits ?? 0).join("");
    case "string":
      return String(value);
    case "lin":
    case "log":
    case "level":
      return formatNorm(t, Number(value));
  }
  return String(value);
}

/** toBits returns the bits as the console writes them: most significant first, bit 0 last. */
export function toBits(value: number, bits: number): number[] {
  const out: number[] = [];
  for (let i = bits - 1; i >= 0; i--) out.push((value >>> i) & 1);
  return out;
}

export const setBit = (value: number, bit: number, on: boolean) => (on ? value | (1 << bit) : value & ~(1 << bit));

/** isOnOff tells the plain OFF/ON enums, shown as toggles. */
export const isOnOff = (t: TypeInfo) =>
  t.kind === "enum" && t.names?.length === 2 && t.names[0] === "OFF" && t.names[1] === "ON";

/** sameValue compares raw values, floats within half a step. */
export function sameValue(t: TypeInfo | undefined, a: RawValue | undefined, b: RawValue | undefined): boolean {
  if (a === undefined || b === undefined) return a === b;
  if (t && isFloatKind(t) && typeof a === "number" && typeof b === "number") {
    const tol = t.steps && t.steps > 1 ? 0.5 / (t.steps - 1) : 1e-4;
    return Math.abs(a - b) < tol;
  }
  return a === b || String(a) === String(b);
}

/** prettyText trims the console's padding and shows -oo as −∞. */
export function prettyText(text: string | undefined): string {
  if (text === undefined) return "";
  const t = text.trim();
  if (t === "-oo") return "−∞";
  if (t.length >= 2 && t.startsWith('"') && t.endsWith('"')) return t.slice(1, -1);
  return t;
}

/** Normalized positions of the fader scale marks. */
export const FADER_MARKS: { db: number; label: string }[] = [
  { db: 10, label: "+10" },
  { db: 5, label: "+5" },
  { db: 0, label: "0" },
  { db: -5, label: "-5" },
  { db: -10, label: "-10" },
  { db: -20, label: "-20" },
  { db: -30, label: "-30" },
  { db: -40, label: "-40" },
  { db: -50, label: "-50" },
  { db: -60, label: "-60" },
  { db: -Infinity, label: "-∞" },
];
