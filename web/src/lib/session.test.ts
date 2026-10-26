import { describe, expect, it } from "vitest";
import { addsMarker, damagedText, endsRecording, formatDuration, parseTime, sinceText } from "./session";

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

describe("sessions", () => {
  it("says which lines of the change log could not be read", () => {
    expect(damagedText(undefined)).toBe("");
    expect(damagedText([])).toBe("");
    expect(damagedText([812, 813, 2040])).toBe(
      "3 lines of the change log could not be read: their changes are missing from this session (lines 812-813, 2040).",
    );
    expect(damagedText([1])).toBe("1 line of the change log could not be read: its changes are missing from this session (line 1).");
  });

  it("adds one marker per press of M, however long it is held", () => {
    const m = { key: "m", ctrlKey: false, metaKey: false, altKey: false, repeat: false };
    expect(addsMarker(m)).toBe(true);
    expect(addsMarker({ ...m, key: "M" })).toBe(true);
    expect(addsMarker({ ...m, repeat: true })).toBe(false);
    expect(addsMarker({ ...m, ctrlKey: true })).toBe(false);
    expect(addsMarker({ ...m, key: "n" })).toBe(false);
  });

  it("asks before connecting or disconnecting ends a recording", () => {
    expect(endsRecording("disconnect", null)).toBeNull();
    expect(endsRecording("connect", { recording: false, replaying: false })).toBeNull();
    const rec = { recording: true, replaying: false };
    expect(endsRecording("disconnect", rec)?.body).toBe("A recording is running: disconnecting ends it, with its audio.");
    expect(endsRecording("connect", rec)?.body).toBe("Connecting to another console ends the recording, with its audio.");
  });

  it("says how long ago tabula started", () => {
    const now = new Date("2026-10-26T22:00:00");
    expect(sinceText(new Date("2026-10-26T21:57:30"), now)).toBe("2 min");
    expect(sinceText(new Date("2026-10-26T19:55:00"), now)).toBe("2 h 05");
    expect(sinceText(new Date("2026-10-22T08:00:00"), now)).toBe("4 days");
    expect(sinceText(new Date("2026-10-26T22:00:30"), now)).toBe("0 min"); // a clock a little ahead
  });
});
