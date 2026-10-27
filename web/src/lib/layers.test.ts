import { describe, expect, it } from "vitest";
import { layerStrips, newLayerId, spillOf } from "./layers";

describe("layers", () => {
  it("keeps the console's order and ids of their own", () => {
    expect(layerStrips({ id: "l1", name: "Vx", strips: ["/bus/01", "/ch/12", "/nope"] }).map((s) => s.path)).toEqual(["/ch/12", "/bus/01"]);
    expect(newLayerId([{ id: "l2", name: "", strips: [] }])).toBe("l3");
    expect(newLayerId([{ id: "l2", name: "", strips: [] }, { id: "l3", name: "", strips: [] }])).toBe("l4");
    expect(newLayerId([])).toBe("l1");
  });

  it("spills the strips of a DCA, then the DCA", () => {
    const maps: Record<string, number> = { "/ch/12": 0b100, "/ch/13": 0b101, "/bus/02": 0b100, "/ch/01": 0b1 };
    expect(spillOf(3, (p) => maps[p]).map((s) => s.path)).toEqual(["/ch/12", "/ch/13", "/bus/02", "/dca/3"]);
    expect(spillOf(8, (p) => maps[p]).map((s) => s.path)).toEqual(["/dca/8"]);
  });
});
