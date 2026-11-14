import { describe, expect, it } from "vitest";
import type { LocalStatus } from "./api";
import { listenChoices, listenKey, localAt } from "./local";

const base: LocalStatus = {
  session: "show",
  duration: 100,
  at: 10,
  at_ms: 1_000_000,
  playing: false,
  speed: 1,
  audio: true,
  main: [0, 0],
  listen: "off",
  heard: false,
};

describe("local replay", () => {
  it("moves on from the status while playing", () => {
    expect(localAt(base, 1_005_000)).toBe(10);
    expect(localAt({ ...base, playing: true }, 1_005_000)).toBe(15);
    expect(localAt({ ...base, playing: true, speed: 4 }, 1_005_000)).toBe(30);
    expect(localAt({ ...base, playing: true }, 2_000_000)).toBe(100); // not past the end
  });

  it("lists what can be listened to", () => {
    const tracks = [
      { channel: 1, name: "Hang", file: "audio/01 Hang.wav" },
      { channel: 2, name: "not recorded" },
      { channel: 31, name: "PA L", file: "a" },
      { channel: 32, name: "PA R", file: "b" },
    ];
    expect(listenChoices(base, tracks).map((c) => c.key)).toEqual(["off", "t1", "t31", "t32"]);
    const withMain = { ...base, main: [31, 32] as [number, number] };
    const choices = listenChoices(withMain, tracks);
    expect(choices[1]).toMatchObject({ key: "stereo", label: "Stereo out (tracks 31 and 32)" });
    expect(choices.find((c) => c.key === "t1")?.label).toBe("Track 1 · Hang");
    expect(listenKey({ ...base, listen: "tracks", tracks: [{ track: 1, gain: 1, pan: 0 }] })).toBe("t1");
    expect(listenKey({ ...withMain, listen: "stereo" })).toBe("stereo");
    expect(listenKey(base)).toBe("off");
  });
});
