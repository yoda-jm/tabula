import { describe, expect, it } from "vitest";
import {
  addTap,
  beatMs,
  bpmOf,
  formatBpm,
  hasTempo,
  isTapKey,
  noteBeats,
  parseBpm,
  targetValue,
  tempoOfValue,
  tempoTargets,
} from "./tempo";

const tapAll = (times: number[]) => times.reduce<number[]>((taps, t) => addTap(taps, t), []);

describe("tap tempo", () => {
  it("averages the last intervals", () => {
    expect(beatMs(tapAll([0]))).toBeNull();
    expect(beatMs(tapAll([0, 500]))).toBe(500);
    // a little off each time: the average
    expect(beatMs(tapAll([0, 490, 1010, 1500]))).toBe(500);
    // only the last 4 intervals count
    const taps = tapAll([0, 600, 1150, 1650, 2150, 2650, 3150]);
    expect(taps).toHaveLength(5);
    expect(beatMs(taps)).toBe(500);
  });

  it("starts over after a pause or a tap off the beat", () => {
    expect(tapAll([0, 500, 1000, 5000])).toEqual([5000]);
    // 500 ms, then 250: a new tempo from the last tap
    expect(tapAll([0, 500, 1000, 1250])).toEqual([1000, 1250]);
    expect(tapAll([0, 500, 500])).toEqual([500]);
  });

  it("converts tempos to times and speeds", () => {
    expect(bpmOf(500)).toBe(120);
    expect(targetValue("time", 120, noteBeats("1/4"))).toBe(500);
    expect(targetValue("time", 120, noteBeats("1/8."))).toBe(375);
    expect(targetValue("time", 120, noteBeats("1/4T"))).toBeCloseTo(333.33);
    expect(targetValue("rate", 120, noteBeats("1/4"))).toBe(2);
    expect(targetValue("rate", 120, noteBeats("1/8"))).toBe(4);
    expect(targetValue("rate", 120, noteBeats("1/1"))).toBe(0.5);
    expect(tempoOfValue("time", 375, 0.75)).toBe(120);
    expect(tempoOfValue("rate", 4, 0.5)).toBe(120);
    expect(noteBeats("nope")).toBe(1);
    expect(formatBpm(120)).toBe("120");
    expect(formatBpm(92.54)).toBe("92.5");
  });

  it("finds what a tempo sets in an effect", () => {
    const dly = [
      { name: "01", label: "Mix" },
      { name: "02", label: "Time" },
    ];
    expect(tempoTargets("DLY", dly)).toEqual([{ field: "02", label: "Time", kind: "time", on: true }]);
    // a delay with a modulation: the time follows, the speed when ticked
    const dcr = [
      { name: "01", label: "Time" },
      { name: "07", label: "Speed" },
    ];
    expect(tempoTargets("D/CR", dcr).map((t) => [t.field, t.kind, t.on])).toEqual([
      ["01", "time", true],
      ["07", "rate", false],
    ]);
    // a modulation alone: its speed follows
    expect(tempoTargets("PAN", [{ name: "01", label: "Speed" }])[0].on).toBe(true);
    // the rotary's speeds, the dynamics' times are not musical
    expect(tempoTargets("ROTA", [{ name: "01", label: "Lo Speed" }])).toEqual([]);
    expect(hasTempo("ROTA")).toBe(false);
    expect(hasTempo("HALL")).toBe(false);
    expect(hasTempo("MODD")).toBe(true);
  });

  it("reads typed tempos and the T key", () => {
    expect(parseBpm(" 120 ")).toBe(120);
    expect(parseBpm("92,5")).toBe(92.5);
    expect(parseBpm("10")).toBeNaN();
    expect(parseBpm("400")).toBeNaN();
    expect(parseBpm("fast")).toBeNaN();
    const key = { key: "t", ctrlKey: false, metaKey: false, altKey: false, repeat: false };
    expect(isTapKey(key)).toBe(true);
    expect(isTapKey({ ...key, key: "T" })).toBe(true);
    expect(isTapKey({ ...key, ctrlKey: true })).toBe(false);
    expect(isTapKey({ ...key, repeat: true })).toBe(false);
  });
});
