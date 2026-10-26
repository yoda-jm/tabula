// Approximate frequency responses of the console's EQ bands, for drawing.
// PEQ, shelves and cuts use the RBJ "Audio EQ Cookbook" biquads at 48 kHz;
// VEQ is drawn as a PEQ; the matrix/main crossover types (BU = Butterworth,
// BS = Bessel, drawn as Butterworth, LR = Linkwitz-Riley) use their ideal
// magnitude. The console's own filters differ in the details.

export const SAMPLE_RATE = 48000;

export interface Band {
  type: string; // LCut, LShv, PEQ, VEQ, HShv, HCut, BU6…LR24
  freq: number; // Hz
  gain: number; // dB
  q: number;
  /** For crossover types: true if the band cuts the lows (a high-pass). */
  highPass?: boolean;
}

interface Biquad {
  b0: number;
  b1: number;
  b2: number;
  a0: number;
  a1: number;
  a2: number;
}

export function biquad(type: string, f0: number, gain: number, q: number, fs = SAMPLE_RATE): Biquad | null {
  const w0 = (2 * Math.PI * Math.min(f0, fs * 0.49)) / fs;
  const cos = Math.cos(w0);
  const sin = Math.sin(w0);
  const Q = Math.max(q, 0.05);
  const alpha = sin / (2 * Q);
  const A = Math.pow(10, gain / 40);
  switch (type) {
    case "PEQ":
    case "VEQ":
      return { b0: 1 + alpha * A, b1: -2 * cos, b2: 1 - alpha * A, a0: 1 + alpha / A, a1: -2 * cos, a2: 1 - alpha / A };
    case "LShv": {
      const s = 2 * Math.sqrt(A) * alpha;
      return {
        b0: A * (A + 1 - (A - 1) * cos + s),
        b1: 2 * A * (A - 1 - (A + 1) * cos),
        b2: A * (A + 1 - (A - 1) * cos - s),
        a0: A + 1 + (A - 1) * cos + s,
        a1: -2 * (A - 1 + (A + 1) * cos),
        a2: A + 1 + (A - 1) * cos - s,
      };
    }
    case "HShv": {
      const s = 2 * Math.sqrt(A) * alpha;
      return {
        b0: A * (A + 1 + (A - 1) * cos + s),
        b1: -2 * A * (A - 1 + (A + 1) * cos),
        b2: A * (A + 1 + (A - 1) * cos - s),
        a0: A + 1 - (A - 1) * cos + s,
        a1: 2 * (A - 1 - (A + 1) * cos),
        a2: A + 1 - (A - 1) * cos - s,
      };
    }
    case "LCut": {
      const a = sin / (2 * Math.SQRT1_2);
      return { b0: (1 + cos) / 2, b1: -(1 + cos), b2: (1 + cos) / 2, a0: 1 + a, a1: -2 * cos, a2: 1 - a };
    }
    case "HCut": {
      const a = sin / (2 * Math.SQRT1_2);
      return { b0: (1 - cos) / 2, b1: 1 - cos, b2: (1 - cos) / 2, a0: 1 + a, a1: -2 * cos, a2: 1 - a };
    }
  }
  return null;
}

/** biquadDb is the magnitude of a biquad at frequency f, in dB. */
export function biquadDb(c: Biquad, f: number, fs = SAMPLE_RATE): number {
  const w = (2 * Math.PI * f) / fs;
  const c1 = Math.cos(w);
  const s1 = Math.sin(w);
  const c2 = Math.cos(2 * w);
  const s2 = Math.sin(2 * w);
  const nr = c.b0 + c.b1 * c1 + c.b2 * c2;
  const ni = -(c.b1 * s1 + c.b2 * s2);
  const dr = c.a0 + c.a1 * c1 + c.a2 * c2;
  const di = -(c.a1 * s1 + c.a2 * s2);
  return 10 * Math.log10((nr * nr + ni * ni) / (dr * dr + di * di));
}

/** butterworthDb is an ideal Butterworth high- or low-pass of the given order. */
export function butterworthDb(f: number, fc: number, order: number, highPass: boolean): number {
  const r = highPass ? fc / f : f / fc;
  return -10 * Math.log10(1 + Math.pow(r, 2 * order));
}

const CROSSOVER = /^(BU|BS|LR)(6|12|18|24)$/;

/** bandDb is the response of one band at frequency f, in dB. */
export function bandDb(b: Band, f: number): number {
  const m = CROSSOVER.exec(b.type);
  if (m) {
    const order = Number(m[2]) / 6;
    const hp = b.highPass ?? true;
    // Linkwitz-Riley: two cascaded Butterworth filters of half the order.
    return m[1] === "LR" ? 2 * butterworthDb(f, b.freq, order / 2, hp) : butterworthDb(f, b.freq, order, hp);
  }
  const c = biquad(b.type, b.freq, b.gain, b.q);
  return c ? biquadDb(c, f) : 0;
}

/** logFrequencies returns n frequencies spread evenly on a log axis. */
export function logFrequencies(n: number, lo = 20, hi = 20000): number[] {
  const out: number[] = [];
  for (let i = 0; i < n; i++) out.push(lo * Math.pow(hi / lo, i / (n - 1)));
  return out;
}

/** response sums the bands (and optional extra filters) over the given frequencies. */
export function response(bands: Band[], freqs: number[], extra: ((f: number) => number)[] = []): number[] {
  return freqs.map((f) => {
    let db = 0;
    for (const b of bands) db += bandDb(b, f);
    for (const e of extra) db += e(f);
    return db;
  });
}

/** A plot of a response: frequencies on a log axis, dB on a linear one, ±range dB in view. */
export interface Plot {
  left: number;
  right: number;
  top: number;
  bottom: number;
  fLo: number;
  fHi: number;
  range: number;
}

export const plotX = (p: Plot, f: number) => p.left + ((p.right - p.left) * Math.log(f / p.fLo)) / Math.log(p.fHi / p.fLo);
export const plotFreq = (p: Plot, x: number) => p.fLo * Math.pow(p.fHi / p.fLo, (x - p.left) / (p.right - p.left));

/** plotY is not clamped to the range: what is out of it is drawn out of the plot (and clipped). */
export const plotY = (p: Plot, db: number) => p.top + ((p.bottom - p.top) * (p.range - db)) / (2 * p.range);
export const plotDb = (p: Plot, y: number) => p.range - (2 * p.range * (y - p.top)) / (p.bottom - p.top);

/** hasGain is true for the band types with a gain (a handle drags it up and down). */
export const hasGain = (type: string) => type === "PEQ" || type === "VEQ" || type === "LShv" || type === "HShv";
