import { describe, expect, it } from "vitest";
import type { AudioConfig } from "./audio";
import { byteRate, exportUrl, formatBytes, formatHours, parseTracks, trackLabel } from "./audio";

describe("audio tracks", () => {
  it("reads track lists as the server does", () => {
    expect(parseTracks("", 4)).toEqual([1, 2, 3, 4]);
    expect(parseTracks("all", 2)).toEqual([1, 2]);
    expect(parseTracks("1-3, 12,2", 16)).toEqual([1, 2, 3, 12]);
    expect(parseTracks("3;1", 4)).toEqual([1, 3]);
    expect(parseTracks("0", 4)).toMatch(/outside/);
    expect(parseTracks("3-1", 4)).toMatch(/outside/);
    expect(parseTracks("1-40", 32)).toMatch(/outside 1-32/);
    expect(parseTracks("a", 4)).toMatch(/not a channel/);
  });
  it("computes the data rate", () => {
    const c: AudioConfig = { enabled: true, recorder: "arecord", device: "", channels: 32, rate: 48000, bits: 24, tracks: "1-16" };
    expect(byteRate(c)).toBe(16 * 48000 * 3);
    // two sets of 45 minutes, 16 tracks: 12.4 GB
    expect(formatBytes(byteRate(c) * 90 * 60)).toBe("12.4 GB");
    expect(byteRate({ ...c, tracks: "" })).toBe(32 * 48000 * 3);
    expect(byteRate({ ...c, tracks: "99" })).toBe(0);
  });
  it("formats sizes and durations", () => {
    expect(formatBytes(-1)).toBe("unknown");
    expect(formatBytes(2_304_000)).toBe("2.3 MB");
    expect(formatBytes(224_000_000)).toBe("224 MB");
    expect(formatBytes(512_000)).toBe("512 kB");
    expect(formatHours(45 * 60)).toBe("45 min");
    expect(formatHours(3 * 3600 + 5 * 60 + 30)).toBe("3 h 05 min");
    expect(formatHours(-1)).toBe("unknown");
  });
  it("names tracks as their files", () => {
    expect(trackLabel({ channel: 1, name: "Hang", file: "audio/01 Hang.wav" })).toBe("01 Hang");
    expect(trackLabel({ channel: 3, name: "Snare" })).toBe("03 Snare");
    expect(trackLabel({ channel: 11 })).toBe("Track 11");
  });
  it("builds export links", () => {
    expect(exportUrl("2026-10-17_203000", "reaper")).toBe("/api/sessions/2026-10-17_203000/export?format=reaper");
  });
});
