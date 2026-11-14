import { describe, expect, it } from "vitest";
import { compressLine, trackFormats } from "./compress";

describe("compress", () => {
  it("counts the tracks by format", () => {
    expect(
      trackFormats({
        rate: 48000,
        bits: 24,
        channels: 32,
        offset: 0,
        frames: 1,
        tracks: [
          { channel: 1, file: "audio/01 Kick.flac" },
          { channel: 2, file: "audio/02 Snare.wav" },
          { channel: 3, file: "audio/03 Hat.WAV" },
          { channel: 4 },
        ],
      }),
    ).toEqual({ wav: 2, flac: 1 });
    expect(trackFormats(undefined)).toEqual({ wav: 0, flac: 0 });
  });

  it("says where a session's compression is", () => {
    const c = { queue: ["a", "b"], session: "a", track: "03 Hat.wav", done: 2, total: 16, saved: 0 };
    expect(compressLine(c, "a")).toBe("track 3 of 16 (03 Hat.wav)");
    expect(compressLine(c, "b")).toBe("waiting: 1 session before it");
    expect(compressLine(c, "c")).toBe("");
    expect(compressLine({ ...c, paused: "recording" }, "a")).toBe("paused while recording");
    expect(compressLine(null, "a")).toBe("");
  });
});
