import { describe, expect, it } from "vitest";
import { PEAK_HOLD_MS, meterFraction, meterSource, peakHold, toDbfs } from "./meters";
import { stripByPath } from "./strips";

describe("meters", () => {
  it("converts levels to dBFS and meter heights", () => {
    expect(toDbfs(1)).toBe(0);
    expect(toDbfs(0.1)).toBeCloseTo(-20, 9);
    expect(toDbfs(0)).toBe(-Infinity);
    expect(meterFraction(0)).toBe(1);
    expect(meterFraction(3)).toBe(1);
    expect(meterFraction(-30)).toBe(0.5);
    expect(meterFraction(-Infinity)).toBe(0);
  });

  it("finds each strip's meter", () => {
    expect(meterSource(stripByPath("/ch/01")!)).toEqual({ group: "0", indexes: [0] });
    expect(meterSource(stripByPath("/ch/32")!)).toEqual({ group: "0", indexes: [31] });
    expect(meterSource(stripByPath("/auxin/01")!)).toEqual({ group: "0", indexes: [32] });
    expect(meterSource(stripByPath("/fxrtn/08")!)).toEqual({ group: "0", indexes: [47] });
    expect(meterSource(stripByPath("/bus/16")!)).toEqual({ group: "0", indexes: [63] });
    expect(meterSource(stripByPath("/mtx/06")!)).toEqual({ group: "0", indexes: [69] });
    expect(meterSource(stripByPath("/main/st")!)).toEqual({ group: "2", indexes: [22, 23] });
    expect(meterSource(stripByPath("/main/m")!)).toEqual({ group: "2", indexes: [24] });
    expect(meterSource(stripByPath("/dca/1")!)).toBeNull();
  });

  it("holds peaks, then lets them fall", () => {
    const p = { db: -Infinity, at: 0 };
    expect(peakHold(p, -6, 0)).toBe(-6);
    expect(peakHold(p, -20, 100)).toBe(-6);
    expect(peakHold(p, -20, PEAK_HOLD_MS + 500)).toBeCloseTo(-16, 9); // 20 dB/s for 0.5 s
    expect(peakHold(p, -3, PEAK_HOLD_MS + 600)).toBe(-3);
  });
});
