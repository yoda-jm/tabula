import { describe, expect, it } from "vitest";
import { GR_MAX, PEAK_HOLD_MS, grDb, grFraction, grSource, grText, meterFraction, meterSource, peakHold, toDbfs } from "./meters";
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
    expect(meterSource(stripByPath("/dca/1")!)).toEqual({ group: "5:0:0", indexes: [16] });
    expect(meterSource(stripByPath("/dca/8")!)).toEqual({ group: "5:0:0", indexes: [23] });
  });

  it("finds each strip's gain reductions", () => {
    expect(grSource(stripByPath("/ch/01")!)).toEqual({ group: "1", gate: 32, dyn: 64, level: 0 });
    expect(grSource(stripByPath("/ch/32")!)).toEqual({ group: "1", gate: 63, dyn: 95, level: 31 });
    expect(grSource(stripByPath("/bus/01")!)).toEqual({ group: "2", dyn: 25, level: 0 });
    expect(grSource(stripByPath("/bus/16")!)).toEqual({ group: "2", dyn: 40, level: 15 });
    expect(grSource(stripByPath("/mtx/06")!)).toEqual({ group: "2", dyn: 46, level: 21 });
    expect(grSource(stripByPath("/main/st")!)).toEqual({ group: "2", dyn: 47, level: 22 });
    expect(grSource(stripByPath("/main/m")!)).toEqual({ group: "2", dyn: 48, level: 24 });
    expect(grSource(stripByPath("/auxin/01")!)).toBeNull();
    expect(grSource(stripByPath("/fxrtn/01")!)).toBeNull();
    expect(grSource(stripByPath("/dca/1")!)).toBeNull();
  });

  it("reads gains as reductions", () => {
    expect(grDb(1)).toBe(0);
    expect(grDb(undefined)).toBe(0);
    expect(grDb(0.5)).toBeCloseTo(6.02, 2);
    expect(grDb(0.01)).toBeCloseTo(40, 9);
    expect(grDb(0)).toBe(GR_MAX);
    expect(grFraction(15)).toBe(0.5);
    expect(grFraction(45)).toBe(1);
    expect(grText(0)).toBe("—");
    expect(grText(4.24)).toBe("4.2");
    expect(grText(GR_MAX)).toBe("≥60");
  });

  it("holds peaks, then lets them fall", () => {
    const p = { db: -Infinity, at: 0 };
    expect(peakHold(p, -6, 0)).toBe(-6);
    expect(peakHold(p, -20, 100)).toBe(-6);
    expect(peakHold(p, -20, PEAK_HOLD_MS + 500)).toBeCloseTo(-16, 9); // 20 dB/s for 0.5 s
    expect(peakHold(p, -3, PEAK_HOLD_MS + 600)).toBe(-3);
  });
});
