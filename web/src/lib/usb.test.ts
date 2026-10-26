import { describe, expect, it } from "vitest";
import { entryAction, formatClock, isPlaying, recordsToStick } from "./usb";

describe("USB stick", () => {
  it("shows times as the console does", () => {
    expect(formatClock(0)).toBe("0:00");
    expect(formatClock(204)).toBe("3:24");
    expect(formatClock(3700)).toBe("1:01:40");
    expect(formatClock(-3)).toBe("0:00");
  });

  it("says what opening an entry does", () => {
    expect(entryAction("dir")).toEqual({ label: "Open" });
    expect(entryAction("wav")).toEqual({ label: "▶ Play" });
    expect(entryAction("scene")).toEqual({ label: "Load…", asks: true });
    expect(entryAction("show")).toBeNull();
    expect(entryAction("file")).toBeNull();
  });

  it("finds the file playing in the directory shown", () => {
    expect(isPlaying("/02 Opening.wav", "", "02 Opening.wav")).toBe(true);
    expect(isPlaying("/Breaks/Interval music.wav", "Breaks", "Interval music.wav")).toBe(true);
    expect(isPlaying("/Breaks/Interval music.wav", "", "Interval music.wav")).toBe(false);
    expect(isPlaying("/a/Breaks/x.wav", "Breaks", "x.wav")).toBe(true);
    expect(isPlaying("", "", "x.wav")).toBe(false);
  });

  it("asks before stopping a recording to the stick only", () => {
    expect(recordsToStick("RECORD")).toBe(true);
    expect(recordsToStick("RPAUSE")).toBe(true);
    expect(recordsToStick("PLAY")).toBe(false);
    expect(recordsToStick("PPAUSE")).toBe(false);
  });
});
