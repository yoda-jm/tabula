import { describe, expect, it } from "vitest";
import type { TypeInfo } from "./api";
import {
  dbToLevel,
  decimals,
  formatDb,
  formatHz,
  formatNorm,
  formatValue,
  fromDisplay,
  levelToDb,
  prettyText,
  quantize,
  round,
  sameValue,
  setBit,
  toBits,
  toDisplay,
} from "./values";

const fader: TypeInfo = { kind: "level", describe: "level [-oo, +10, 1024] dB", steps: 1024, unit: "dB" };
const send: TypeInfo = { kind: "level", describe: "level [-oo, +10, 161] dB", steps: 161, unit: "dB" };
const pan: TypeInfo = { kind: "lin", describe: "linf [-100, 100, 2]", min: -100, max: 100, steps: 101 };
const freq: TypeInfo = { kind: "log", describe: "logf [20, 20000, 201] Hz", min: 20, max: 20000, steps: 201, unit: "Hz" };
const trim: TypeInfo = { kind: "lin", describe: "linf [-18, 18, 0.25] dB", min: -18, max: 18, steps: 145, unit: "dB" };
const q: TypeInfo = { kind: "log", describe: "logf [10, 0.3, 72]", min: 10, max: 0.3, steps: 72 };
const hold: TypeInfo = { kind: "log", describe: "logf [0.02, 2000, 101] ms", min: 0.02, max: 2000, steps: 101, unit: "ms" };

describe("fader law", () => {
  it("maps 0.75 to 0 dB and 1 to +10 dB", () => {
    expect(levelToDb(0.75)).toBe(0);
    expect(levelToDb(1)).toBe(10);
    expect(formatNorm(fader, 0.75)).toBe("0.0");
    expect(formatNorm(fader, 1)).toBe("+10.0");
  });
  it("maps 0 to -oo", () => {
    expect(levelToDb(0)).toBe(-Infinity);
    expect(formatNorm(fader, 0)).toBe("-oo");
  });
  it("maps the first step above zero to -89.5 dB", () => {
    expect(levelToDb(1 / 1023)).toBeCloseTo(-89.53, 2);
    expect(formatNorm(fader, 1 / 1023)).toBe("-89.5");
  });
  it("meets at -60, -30 and -10 dB", () => {
    expect(levelToDb(0.0625)).toBe(-60);
    expect(levelToDb(0.25)).toBe(-30);
    expect(levelToDb(0.5)).toBe(-10);
  });
  it("inverts", () => {
    for (const db of [-85, -60, -45, -30, -20, -10, -3, 0, 4.5, 10]) {
      expect(levelToDb(dbToLevel(db))).toBeCloseTo(db, 9);
    }
    expect(dbToLevel(-Infinity)).toBe(0);
    expect(dbToLevel(-120)).toBe(0);
    expect(dbToLevel(20)).toBe(1);
  });
  it("quantizes sends to 161 steps", () => {
    expect(quantize(0.7531, 161)).toBeCloseTo(0.75, 9);
    expect(formatNorm(send, 0.7531)).toBe("0.0");
    expect(fromDisplay(send, -20)).toBeCloseTo(0.375, 9);
  });
});

describe("linear and log scales", () => {
  it("pan 0.75 is +50", () => {
    expect(toDisplay(pan, 0.75)).toBe(50);
    expect(formatNorm(pan, 0.75)).toBe("+50");
    expect(formatNorm(pan, 0.5)).toBe("+0");
    expect(formatNorm(pan, 0)).toBe("-100");
  });
  it("a 201-step log frequency at 0.5 is 632.5 Hz", () => {
    expect(toDisplay(freq, 0.5)).toBeCloseTo(632.456, 3);
    expect(formatNorm(freq, 0.5)).toBe("632.5");
  });
  it("prints frequencies like the console", () => {
    expect(formatHz(295.8)).toBe("295.8");
    expect(formatHz(1970)).toBe("1k97");
    expect(formatHz(10020)).toBe("10k02");
    expect(formatHz(20000)).toBe("20k00");
    // exp(ln(1000)) is a hair below 1000, and the server prints 19k99 too
    expect(formatNorm(freq, 1)).toBe("19k99");
    expect(formatNorm(freq, 0)).toBe("20.0");
  });
  it("prints trims with a sign and the step's decimals", () => {
    expect(decimals(trim)).toBe(2);
    expect(formatNorm(trim, 0.5)).toBe("+0.00");
    expect(decimals(pan)).toBe(0);
  });
  it("handles decreasing log scales (EQ Q)", () => {
    expect(toDisplay(q, 0)).toBeCloseTo(10, 9);
    expect(toDisplay(q, 1)).toBeCloseTo(0.3, 9);
    expect(fromDisplay(q, 2)).toBeCloseTo(quantize(Math.log(2 / 10) / Math.log(0.3 / 10), 72), 9);
  });
  it("round-trips through fromDisplay", () => {
    expect(fromDisplay(freq, 1000)).toBeCloseTo(113 / 200, 9);
    // 1 kHz is not a known value: the nearest step is 990.9 Hz, as in scene files
    expect(formatNorm(freq, fromDisplay(freq, 1000))).toBe("990.9");
    expect(fromDisplay(pan, 50)).toBe(0.75);
  });
  it("prints three significant digits on other log scales", () => {
    expect(formatNorm(hold, 0)).toBe("0.02");
    expect(formatNorm(hold, 1)).toBe("2000");
  });
});

describe("quantize and rounding", () => {
  it("clamps and snaps", () => {
    expect(quantize(-1, 10)).toBe(0);
    expect(quantize(2, 10)).toBe(1);
    expect(quantize(0.3333, 4)).toBeCloseTo(1 / 3, 9);
    expect(quantize(0.123456, 0)).toBe(0.123456);
    expect(quantize(Number.NaN, 5)).toBe(0);
  });
  it("rounds ties away from zero, without negative zero", () => {
    expect(round(-8.25, 1)).toBe(-8.3);
    expect(round(8.25, 1)).toBe(8.3);
    expect(Object.is(round(-0.01, 1), 0)).toBe(true);
    expect(formatDb(-0.04)).toBe("0.0");
    expect(formatDb(0.3)).toBe("+0.3");
  });
});

describe("other kinds", () => {
  const onoff: TypeInfo = { kind: "enum", describe: "", names: ["OFF", "ON"] };
  const bits: TypeInfo = { kind: "bitmap", describe: "", bits: 8 };
  it("prints enums and bitmaps", () => {
    expect(formatValue(onoff, 1)).toBe("ON");
    expect(formatValue(bits, 5)).toBe("%00000101");
    expect(toBits(5, 4)).toEqual([0, 1, 0, 1]);
    expect(setBit(5, 1, true)).toBe(7);
    expect(setBit(5, 0, false)).toBe(4);
  });
  it("compares floats within half a step", () => {
    expect(sameValue(fader, 0.5, 0.5004887580871582)).toBe(true);
    expect(sameValue(fader, 0.5, 0.502)).toBe(false);
    expect(sameValue(onoff, 1, 1)).toBe(true);
    expect(sameValue(onoff, 1, 0)).toBe(false);
  });
  it("prettifies console text", () => {
    expect(prettyText("  -oo")).toBe("−∞");
    expect(prettyText(' "Vx Remy"')).toBe("Vx Remy");
    expect(prettyText(" -6.0")).toBe("-6.0");
  });
});
