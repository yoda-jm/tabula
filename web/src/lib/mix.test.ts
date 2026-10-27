import { describe, expect, it } from "vitest";
import { masterBuses, mixSources, panLabel } from "./mix";
import { ALL_STRIPS } from "./strips";

const SOURCES = ALL_STRIPS.filter((s) => s.kind === "ch").slice(0, 6);
const NAMES: Record<string, string> = { "/ch/01": "Kick", "/ch/02": "", "/ch/03": "Gtr L", "/ch/04": "Gtr R", "/ch/06": "Voice" };
const info = (s: { path: string }) => ({ name: NAMES[s.path] ?? "", colour: s.path === "/ch/06" ? 3 : 1 });
const none = () => false;

describe("mixSources", () => {
  it("leaves out unnamed sources unless asked", () => {
    expect(mixSources(SOURCES, none, info, { showAll: false, pins: [] }).map((s) => s.strip.path)).toEqual(["/ch/01", "/ch/03", "/ch/04", "/ch/06"]);
    expect(mixSources(SOURCES, none, info, { showAll: true, pins: [] })).toHaveLength(6);
  });

  it("shows a linked pair as one source", () => {
    const linked = (flag: string) => flag === "/config/chlink/3-4" || flag === "/config/chlink/5-6";
    const got = mixSources(SOURCES, linked, info, { showAll: false, pins: [] });
    expect(got.map((s) => [s.strip.path, s.partner?.path, s.label, s.name])).toEqual([
      ["/ch/01", undefined, "Ch 01", "Kick"],
      ["/ch/03", "/ch/04", "Ch 03-04", "Gtr L"],
      ["/ch/05", "/ch/06", "Ch 05-06", "Voice"], // named after the even strip when only it has a name
    ]);
    expect(got[2].colour).toBe(3);
  });

  it("puts the pinned sources first, named or not", () => {
    const got = mixSources(SOURCES, none, info, { showAll: false, pins: ["/ch/06", "/ch/02"] });
    expect(got.map((s) => s.strip.path)).toEqual(["/ch/02", "/ch/06", "/ch/01", "/ch/03", "/ch/04"]);
  });
});

describe("masterBuses", () => {
  it("writes both buses of a stereo mix unless the console links their faders and mutes", () => {
    expect(masterBuses([3, 4], true)).toEqual([3]);
    expect(masterBuses([3, 4], false)).toEqual([3, 4]);
    expect(masterBuses([5], false)).toEqual([5]);
  });
});

describe("panLabel", () => {
  it("reads like the console's pan scale", () => {
    expect(["-100", "-40", "+0", "0", "+25", "+100"].map(panLabel)).toEqual(["L100", "L40", "C", "C", "R25", "R100"]);
  });
});
