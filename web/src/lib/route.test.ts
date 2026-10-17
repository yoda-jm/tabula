import { describe, expect, it } from "vitest";
import { formatRoute, parseRoute } from "./route";

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
    expect(parseRoute("#/nowhere")).toBeNull();
  });
});
