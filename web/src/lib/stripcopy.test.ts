import { describe, expect, it } from "vitest";
import type { Schema } from "./schema";
import { copySectionsOf, copyTargets } from "./stripcopy";
import { stripByPath } from "./strips";

const schemaOf = (paths: string[]) => ({ byPath: new Map(paths.map((p) => [p, {}])) }) as unknown as Schema;

describe("strip copy", () => {
  it("offers the sections the strip has", () => {
    const ch = stripByPath("/ch/01")!;
    const schema = schemaOf(["/ch/01/config", "/ch/01/preamp", "/ch/01/gate", "/ch/01/dyn", "/ch/01/eq", "/ch/01/mix"]);
    expect(copySectionsOf(schema, ch)).toEqual(["preamp", "gate", "dyn", "eq", "main"]);
    expect(copySectionsOf(schemaOf(["/dca/1"]), stripByPath("/dca/1")!)).toEqual([]);
  });

  it("copies to the other strips of its kind", () => {
    const t = copyTargets(stripByPath("/ch/05")!);
    expect(t.length).toBe(31);
    expect(t.some((s) => s.path === "/ch/05" || s.kind !== "ch")).toBe(false);
    expect(copyTargets(stripByPath("/fxrtn/01")!).map((s) => s.path)).toHaveLength(7);
    expect(copyTargets(stripByPath("/main/st")!)).toEqual([]);
  });
});
