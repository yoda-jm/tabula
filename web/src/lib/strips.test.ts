import { describe, expect, it } from "vitest";
import type { SchemaResponse } from "./api";
import { indexSchema, parentOf, sectionOf } from "./schema";
import { ALL_STRIPS, BANKS, colourOf, eqBandsOf, linkOf, pairLabel, pairStrips, sectionsOf, sendsOf, sourceLabel, stripAddrs, stripByPath } from "./strips";

const t = { kind: "enum" as const, describe: "" };
const node = (path: string, ...fields: string[]) => ({ path, scene: true, fields: fields.map((name) => ({ name, type: 0 })) });

const schema = indexSchema({
  model: "M32C",
  types: [t],
  nodes: [
    node("/ch/01/config", "name", "icon", "color", "source"),
    node("/ch/01/preamp", "trim", "invert", "hpon", "hpslope", "hpf"),
    node("/ch/01/gate", "on"),
    node("/ch/01/dyn", "on"),
    node("/ch/01/eq", "on"),
    node("/ch/01/eq/1", "type", "f", "g", "q"),
    node("/ch/01/eq/2", "type", "f", "g", "q"),
    node("/ch/01/mix", "on", "fader", "st", "pan"),
    node("/ch/01/mix/01", "on", "level", "pan", "type"),
    node("/ch/01/mix/02", "on", "level"),
    node("/ch/01/grp", "dca", "mute"),
    node("/mtx/01/config", "name", "icon", "color"),
    node("/mtx/01/mix", "on", "fader"),
    node("/bus/01/mix", "on", "fader"),
    node("/bus/01/mix/01", "on", "level"),
    node("/dca/1", "on", "fader"),
    node("/dca/1/config", "name", "icon", "color"),
  ],
} as SchemaResponse);

describe("strips", () => {
  it("lists the banks", () => {
    expect(BANKS.map((b) => b.strips.length)).toEqual([16, 16, 16, 16, 8, 8]);
    expect(ALL_STRIPS).toHaveLength(80);
    expect(stripByPath("/fxrtn/04")?.label).toBe("FX 2R");
    expect(stripByPath("/main/m")?.label).toBe("M/C");
  });

  it("finds the strip's addresses in the schema", () => {
    expect(stripAddrs(schema, stripByPath("/ch/01")!)).toEqual({
      name: "/ch/01/config/name",
      color: "/ch/01/config/color",
      fader: "/ch/01/mix/fader",
      on: "/ch/01/mix/on",
      pan: "/ch/01/mix/pan",
    });
    expect(stripAddrs(schema, stripByPath("/mtx/01")!).pan).toBeUndefined();
    expect(stripAddrs(schema, stripByPath("/dca/1")!)).toMatchObject({ fader: "/dca/1/fader", on: "/dca/1/on" });
  });

  it("lists only the sections a strip has", () => {
    expect(sectionsOf(schema, stripByPath("/ch/01")!)).toEqual(["config", "preamp", "gate", "dyn", "eq", "main", "sends", "groups"]);
    expect(sectionsOf(schema, stripByPath("/mtx/01")!)).toEqual(["config", "main"]);
    expect(sectionsOf(schema, stripByPath("/dca/1")!)).toEqual(["config", "main"]);
    expect(eqBandsOf(schema, stripByPath("/ch/01")!)).toEqual(["/ch/01/eq/1", "/ch/01/eq/2"]);
  });

  it("sends go to buses from inputs, to matrices from buses", () => {
    const s = sendsOf(schema, stripByPath("/ch/01")!);
    expect(s.map((x) => x.target.path)).toEqual(["/bus/01", "/bus/02"]);
    expect(sendsOf(schema, stripByPath("/bus/01")!)[0].target).toEqual({ path: "/mtx/01", label: "Mtx 1" });
  });

  it("names colours and sources", () => {
    expect(colourOf(1)).toEqual({ name: "RD", inverted: false });
    expect(colourOf(12)).toEqual({ name: "BL", inverted: true });
    expect(sourceLabel(0)).toBe("OFF");
    expect(sourceLabel(5)).toBe("In 05");
    expect(sourceLabel(33)).toBe("Aux 1");
    expect(sourceLabel(39)).toBe("USB L");
    expect(sourceLabel(41)).toBe("FX 1L");
    expect(sourceLabel(44)).toBe("FX 2R");
    expect(sourceLabel(64)).toBe("Bus 16");
  });

  it("splits paths", () => {
    expect(sectionOf("/ch/01/mix")).toBe("/ch");
    expect(sectionOf("/-prefs/style")).toBe("/-prefs");
    expect(parentOf("/ch/01/mix/fader")).toBe("/ch/01/mix");
    expect(schema.sections).toEqual(["/ch", "/mtx", "/bus", "/dca"]);
  });

  it("finds linked pairs", () => {
    expect(linkOf(stripByPath("/ch/02")!)).toEqual({ flag: "/config/chlink/1-2", odd: "/ch/01", even: "/ch/02" });
    expect(linkOf(stripByPath("/auxin/07")!)).toEqual({ flag: "/config/auxlink/7-8", odd: "/auxin/07", even: "/auxin/08" });
    expect(linkOf(stripByPath("/fxrtn/04")!)?.flag).toBe("/config/fxlink/3-4");
    expect(linkOf(stripByPath("/bus/16")!)?.flag).toBe("/config/buslink/15-16");
    expect(linkOf(stripByPath("/mtx/05")!)?.flag).toBe("/config/mtxlink/5-6");
    expect(linkOf(stripByPath("/main/st")!)).toBeNull();
    expect(linkOf(stripByPath("/dca/1")!)).toBeNull();
    expect(pairLabel(stripByPath("/fxrtn/03")!)).toBe("FX 2");
    expect(pairLabel(stripByPath("/ch/31")!)).toBe("Ch 31-32");
    expect(pairLabel(stripByPath("/mtx/03")!)).toBe("Mtx 3-4");
  });

  it("merges linked pairs of a bank into stereo strips", () => {
    // GVO-2026: matrices 1-2 and 3-4 linked, aux 7-8, the FX returns
    const on = new Set(["/config/mtxlink/1-2", "/config/mtxlink/3-4", "/config/auxlink/7-8", "/config/fxlink/1-2", "/config/fxlink/3-4"]);
    const main = pairStrips(BANKS[4].strips, (f) => on.has(f));
    expect(main.map((m) => m.label)).toEqual(["Mtx 1-2", "Mtx 3-4", "Mtx 5", "Mtx 6", "LR", "M/C"]);
    expect(main[0].partner?.path).toBe("/mtx/02");
    const aux = pairStrips(BANKS[2].strips, (f) => on.has(f));
    expect(aux.map((m) => m.label)).toEqual(["Aux 1", "Aux 2", "Aux 3", "Aux 4", "Aux 5", "Aux 6", "Aux 7-8", "FX 1", "FX 2", "FX 3L", "FX 3R", "FX 4L", "FX 4R"]);
    expect(pairStrips(BANKS[0].strips, () => false)).toHaveLength(16);
  });
});
