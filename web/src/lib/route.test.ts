import { afterEach, describe, expect, it, vi } from "vitest";
import { followKey, formatRoute, parseRoute } from "./route";

describe("routes", () => {
  it("reads and writes the FX page with its slot", () => {
    expect(parseRoute("#/fx")).toEqual({ page: "fx" });
    expect(parseRoute("#/fx/5")).toEqual({ page: "fx", slot: 5 });
    expect(parseRoute("#/fx/9")).toEqual({ page: "fx" });
    expect(formatRoute({ page: "fx", slot: 3 })).toBe("#/fx/3");
    expect(formatRoute({ page: "fx" })).toBe("#/fx");
  });

  it("keeps the other pages", () => {
    expect(parseRoute("#/channel/ch/12")).toEqual({ page: "channel", strip: "/ch/12" });
    expect(parseRoute("#/routing")).toEqual({ page: "routing" });
    expect(parseRoute("#/meters")).toEqual({ page: "meters" });
    expect(parseRoute("#/nowhere")).toBeNull();
  });
});

describe("persistent values", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("reads the value of a new key instead of keeping the old one", () => {
    const kept: Record<string, string> = { "tabula.mix.pins.3": JSON.stringify(["/ch/05"]) };
    vi.stubGlobal("window", { localStorage: { getItem: (k: string) => kept[k] ?? null } });
    const held = { key: "tabula.mix.pins.1", v: ["/ch/01"] };
    expect(followKey(held, "tabula.mix.pins.1", [])).toBe(held);
    expect(followKey(held, "tabula.mix.pins.3", [])).toEqual({ key: "tabula.mix.pins.3", v: ["/ch/05"] });
    expect(followKey(held, "tabula.mix.pins.5", [])).toEqual({ key: "tabula.mix.pins.5", v: [] });
  });
});
