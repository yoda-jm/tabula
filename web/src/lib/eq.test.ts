import { describe, expect, it } from "vitest";
import { bandDb, butterworthDb, hasGain, logFrequencies, plotDb, plotFreq, plotX, plotY, response } from "./eq";

describe("EQ responses", () => {
  it("a PEQ reaches its gain at its frequency and is flat far away", () => {
    const b = { type: "PEQ", freq: 1000, gain: 6, q: 2 };
    expect(bandDb(b, 1000)).toBeCloseTo(6, 3);
    expect(Math.abs(bandDb(b, 50))).toBeLessThan(0.05);
    expect(Math.abs(bandDb(b, 15000))).toBeLessThan(0.2);
    expect(bandDb({ ...b, gain: -9 }, 1000)).toBeCloseTo(-9, 3);
  });

  it("a flat PEQ is flat", () => {
    for (const f of logFrequencies(20)) expect(bandDb({ type: "PEQ", freq: 500, gain: 0, q: 1 }, f)).toBeCloseTo(0, 6);
  });

  it("shelves reach their gain on their side", () => {
    expect(bandDb({ type: "LShv", freq: 200, gain: 5, q: 0.7 }, 20)).toBeCloseTo(5, 1);
    expect(Math.abs(bandDb({ type: "LShv", freq: 200, gain: 5, q: 0.7 }, 10000))).toBeLessThan(0.1);
    expect(bandDb({ type: "HShv", freq: 5000, gain: -4, q: 0.7 }, 20000)).toBeCloseTo(-4, 0);
    expect(Math.abs(bandDb({ type: "HShv", freq: 5000, gain: -4, q: 0.7 }, 100))).toBeLessThan(0.1);
  });

  it("cuts are -3 dB at their frequency", () => {
    expect(bandDb({ type: "LCut", freq: 100, gain: 0, q: 1 }, 100)).toBeCloseTo(-3.01, 1);
    expect(bandDb({ type: "LCut", freq: 100, gain: 0, q: 1 }, 10)).toBeLessThan(-35);
    expect(bandDb({ type: "HCut", freq: 8000, gain: 0, q: 1 }, 8000)).toBeCloseTo(-3.01, 1);
    expect(Math.abs(bandDb({ type: "HCut", freq: 8000, gain: 0, q: 1 }, 100))).toBeLessThan(0.01);
  });

  it("crossovers: Butterworth -3 dB, Linkwitz-Riley -6 dB at the crossover", () => {
    expect(bandDb({ type: "BU24", freq: 100, gain: 0, q: 1, highPass: true }, 100)).toBeCloseTo(-3.01, 2);
    expect(bandDb({ type: "LR24", freq: 100, gain: 0, q: 1, highPass: false }, 100)).toBeCloseTo(-6.02, 2);
    // 24 dB/octave one octave away
    expect(butterworthDb(50, 100, 4, true)).toBeCloseTo(-24.1, 0);
  });

  it("sums bands", () => {
    const fs = [1000];
    const r = response(
      [
        { type: "PEQ", freq: 1000, gain: 3, q: 2 },
        { type: "PEQ", freq: 1000, gain: 2, q: 2 },
      ],
      fs,
    );
    expect(r[0]).toBeCloseTo(5, 3);
  });

  it("spreads frequencies on a log axis", () => {
    const f = logFrequencies(4, 10, 10000);
    expect(f[0]).toBeCloseTo(10, 9);
    expect(f[1]).toBeCloseTo(100, 9);
    expect(f[3]).toBeCloseTo(10000, 6);
  });

  it("places frequencies and gains on the plot, and back", () => {
    const p = { left: 34, right: 900, top: 0, bottom: 222, fLo: 20, fHi: 20000, range: 18 };
    expect(plotX(p, 20)).toBeCloseTo(34, 6);
    expect(plotX(p, 20000)).toBeCloseTo(900, 6);
    expect(plotY(p, 18)).toBeCloseTo(0, 6);
    expect(plotY(p, 0)).toBeCloseTo(111, 6);
    for (const f of [20, 63, 1000, 12500]) expect(plotFreq(p, plotX(p, f))).toBeCloseTo(f, 6);
    for (const db of [-15, -2.5, 0, 7.25]) expect(plotDb(p, plotY(p, db))).toBeCloseTo(db, 6);
    // a low cut's slope is not clamped: it runs out of the plot
    expect(plotY(p, -40)).toBeGreaterThan(p.bottom);
  });

  it("knows which bands have a gain", () => {
    expect(["PEQ", "VEQ", "LShv", "HShv"].every(hasGain)).toBe(true);
    expect(["LCut", "HCut", "BU12", "LR24"].some(hasGain)).toBe(false);
  });
});
