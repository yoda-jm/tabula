import { describe, expect, it } from "vitest";
import { playRouting, playableTracks } from "./soundcheck";

describe("playRouting", () => {
  it("lists the inputs taken from the card", () => {
    const values: Record<string, string> = {
      "/config/routing/routswitch": "REC",
      "/config/routing/PLAY/1-8": "CARD1-8",
      "/config/routing/PLAY/9-16": "CARD9-16",
      "/config/routing/PLAY/17-24": "AN17-24",
    };
    expect(playRouting((a) => values[a])).toEqual({ playing: false, card: ["1-8 ← CARD1-8", "9-16 ← CARD9-16"] });
    values["/config/routing/routswitch"] = "PLAY";
    expect(playRouting((a) => values[a]).playing).toBe(true);
    expect(playRouting(() => undefined)).toEqual({ playing: false, card: [] });
  });
});

describe("playableTracks", () => {
  it("counts the tracks with a file", () => {
    expect(playableTracks(undefined)).toBe(0);
    const tracks = [{ channel: 1, file: "audio/01 Hang.wav" }, { channel: 2 }, { channel: 3, file: "audio/03 Snare.wav" }];
    expect(playableTracks({ rate: 48000, bits: 24, channels: 32, tracks, offset: 0, frames: 10 })).toBe(2);
  });
});
