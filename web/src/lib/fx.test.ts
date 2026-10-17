import { describe, expect, it } from "vitest";
import { fxName, fxUsage, splitDual, stripOfIndex } from "./fx";
import type { Lookup } from "./routing";
import {
  geqBandFreq,
  geqPosition,
  geqRtaSource,
  isDualGeq,
  isGeq,
  rtaFraction,
  rtaFreq,
  rtaSourceLabel,
  rtaWindow,
  stripRtaSource,
} from "./rta";

function lookup(v: Record<string, number | string>): Lookup {
  return { text: (a) => (a in v ? String(v[a]) : undefined), value: (a) => v[a] };
}

/** GVO-2026: reverbs on buses 13-16, graphic EQs inserted on matrices 1-4 and buses 7-8. */
const GVO = lookup({
  "/fx/1/type": "VRM",
  "/fx/1/source/l": "MIX13",
  "/fx/1/source/r": "MIX13",
  "/bus/13/config/name": "Fx Room",
  "/fxrtn/01/config/name": "Room L",
  "/fx/5/type": "GEQ2",
  "/-insert/fx5L": 64,
  "/-insert/fx5R": 65,
  "/mtx/01/config/name": "Façade",
  "/fx/7/type": "GEQ2",
  "/-insert/fx7L": 54,
  "/-insert/fx7R": 55,
  "/fx/8/type": "GEQ",
  "/-insert/fx8L": -1,
  "/-insert/fx8R": -1,
});

describe("effects", () => {
  it("names effects", () => {
    expect(fxName("GEQ2")).toBe("Dual Graphic EQ");
    expect(fxName("XYZ")).toBe("XYZ");
  });

  it("maps strip indexes", () => {
    expect(stripOfIndex(0)).toEqual({ path: "/ch/01", label: "Ch 01" });
    expect(stripOfIndex(39)).toEqual({ path: "/auxin/08", label: "Aux 8" });
    expect(stripOfIndex(41)).toEqual({ path: "/fxrtn/02", label: "FX 1R" });
    expect(stripOfIndex(54)).toEqual({ path: "/bus/07", label: "Bus 07" });
    expect(stripOfIndex(64)).toEqual({ path: "/mtx/01", label: "Mtx 1" });
    expect(stripOfIndex(71)).toEqual({ path: "/main/m", label: "M/C" });
    expect(stripOfIndex(-1)).toBeNull();
  });

  it("tells where each slot is used", () => {
    const fx1 = fxUsage(GVO, 1);
    expect(fx1.type).toBe("VRM");
    expect(fx1.sources.map((u) => u.text)).toEqual(["Bus 13 Fx Room", "Bus 13 Fx Room"]);
    expect(fx1.returns.map((u) => u.text)).toEqual(["FX 1L Room L", "FX 1R"]);
    expect(fx1.inserts).toEqual([]);
    expect(fxUsage(GVO, 5).inserts).toEqual([
      { text: "Mtx 1 Façade", strip: "/mtx/01", side: "L" },
      { text: "Mtx 2", strip: "/mtx/02", side: "R" },
    ]);
    expect(fxUsage(GVO, 7).inserts.map((u) => u.text)).toEqual(["Bus 07", "Bus 08"]);
    const fx8 = fxUsage(GVO, 8);
    expect(fx8.inserts).toEqual([]);
    expect(fx8.sources).toEqual([]);
  });

  it("splits dual effects into their two channels", () => {
    expect(splitDual(["Gain A", "Freq A", "Gain B", "Freq B"])).toEqual({ a: [0, 1], b: [2, 3] });
    expect(splitDual(["Pre Delay", "Decay"])).toBeNull();
    expect(splitDual(["Gain A", "Mix"])).toBeNull();
  });
});

describe("RTA", () => {
  it("names sources", () => {
    expect(rtaSourceLabel(11)).toBe("Ch 12 (pre EQ)");
    expect(rtaSourceLabel(11 + 98)).toBe("Ch 12 (post EQ)");
    expect(rtaSourceLabel(54)).toBe("Bus 07 (pre EQ)");
    expect(rtaSourceLabel(70)).toBe("LR (pre EQ)");
    expect(rtaSourceLabel(82)).toBe("FX 5 GEQ A (pre EQ)");
    expect(rtaSourceLabel(83 + 98)).toBe("FX 5 GEQ B (post EQ)");
    expect(rtaSourceLabel(97)).toBe("FX 8 GEQ (pre EQ)");
    expect(rtaSourceLabel(500)).toBe("500");
  });

  it("numbers the graphic EQ and strip sources as docs/emulator.md does", () => {
    expect(isGeq("TEQ2") && isDualGeq("TEQ2") && isGeq("GEQ") && !isDualGeq("GEQ") && !isGeq("HALL")).toBe(true);
    expect(geqRtaSource(1, "GEQ2", 0, false)).toBe(74);
    expect(geqRtaSource(5, "GEQ2", 1, true)).toBe(74 + 8 + 1 + 98);
    expect(geqRtaSource(8, "GEQ", 0, false)).toBe(97);
    expect(geqRtaSource(1, "TEQ", 1, true)).toBe(90 + 98);
    expect(stripRtaSource("/ch/12")).toBe(11);
    expect(stripRtaSource("/auxin/01")).toBe(32);
    expect(stripRtaSource("/bus/07")).toBe(54);
    expect(stripRtaSource("/mtx/06")).toBe(69);
    expect(stripRtaSource("/main/m")).toBe(71);
    expect(stripRtaSource("/dca/1")).toBeNull();
  });

  it("places the RTA bands on the graphic EQ axis", () => {
    expect(rtaFreq(60)).toBe(1250);
    expect(geqBandFreq(17)).toBe(1000);
    expect(geqPosition(geqBandFreq(17))).toBeCloseTo(17.5);
    expect(geqPosition(geqBandFreq(0))).toBeCloseTo(0.5);
    expect(geqPosition(rtaFreq(0))).toBeGreaterThan(0);
    expect(geqPosition(rtaFreq(99))).toBeLessThan(31);
  });

  it("draws in the window of the RTA preferences", () => {
    expect(rtaWindow(18, false, 30)).toEqual({ top: -18, bottom: -48 });
    expect(rtaWindow(18, true, 30)).toEqual({ top: -30, bottom: -90 });
    const w = rtaWindow(18, false, 30);
    expect(rtaFraction(-48, w)).toBe(0);
    expect(rtaFraction(-33, w)).toBe(0.5);
    expect(rtaFraction(0, w)).toBe(1);
    expect(rtaFraction(-Infinity, w)).toBe(0);
  });
});
