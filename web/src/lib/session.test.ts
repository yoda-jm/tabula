import { describe, expect, it } from "vitest";
import { formatDuration, parseTime } from "./session";

describe("session times", () => {
  it("formats durations", () => {
    expect(formatDuration(0)).toBe("0:00");
    expect(formatDuration(59.9)).toBe("0:59");
    expect(formatDuration(61)).toBe("1:01");
    expect(formatDuration(3723)).toBe("1:02:03");
    expect(formatDuration(-5)).toBe("0:00");
  });
  it("reads seconds, m:ss and h:mm:ss", () => {
    expect(parseTime("")).toBe(0);
    expect(parseTime("90")).toBe(90);
    expect(parseTime("1:30")).toBe(90);
    expect(parseTime("1:02:03")).toBe(3723);
    expect(parseTime("12.5")).toBe(12.5);
    expect(parseTime("abc")).toBeNaN();
    expect(parseTime("1:2:3:4")).toBeNaN();
  });
});
