import { describe, expect, it } from "vitest";
import type { CheckLine } from "./check";
import { checkSummary } from "./check";

const line = (id: string, level: CheckLine["level"]): CheckLine => ({ id, title: id, level, text: "" });

describe("checkSummary", () => {
  it("counts what to fix and what to look at", () => {
    const v = { hours: 3, at: "", lines: [line("console", "ok"), line("card", "bad"), line("disk", "bad"), line("power", "warn"), line("sleep", "unknown")] };
    expect(checkSummary(v)).toEqual({ bad: 2, warn: 1, level: "bad", text: "2 to fix, 1 to look at" });
    expect(checkSummary({ ...v, lines: [line("console", "ok"), line("usb", "info"), line("sleep", "unknown")] })).toMatchObject({ level: "ok", text: "all good" });
    expect(checkSummary({ ...v, lines: [line("power", "warn")] })).toMatchObject({ level: "warn", text: "1 to look at" });
  });
});
