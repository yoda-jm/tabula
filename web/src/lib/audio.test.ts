import { describe, expect, it } from "vitest";
import type { AudioConfig, AudioDevice, AudioInfo, AudioStatus } from "./audio";
import {
  audioPlan,
  audioSummary,
  audioTrouble,
  byteRate,
  configChanges,
  exportUrl,
  formatBytes,
  formatHours,
  formatTracks,
  parseTracks,
  plural,
  sameConfig,
  tooManyChannels,
  trackLabel,
} from "./audio";

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
  it("writes track lists back", () => {
    const want = [...Array.from({ length: 16 }, (_, i) => i + 1), 23, 24];
    expect(formatTracks(want, 32)).toBe("1-16,23-24");
    expect(parseTracks(formatTracks(want, 32), 32)).toEqual(want);
    expect(formatTracks([5, 1, 3, 2, 2], 32)).toBe("1-3,5");
    expect(formatTracks([7], 32)).toBe("7");
    expect(formatTracks([1, 2, 3, 4], 4)).toBe(""); // all
    expect(formatTracks([4, 40], 32)).toBe("4");
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

describe("audioPlan", () => {
  const c: AudioConfig = { enabled: true, recorder: "arecord", device: "hw:X32", channels: 32, rate: 48000, bits: 24, tracks: "1-16" };
  it("says what the next session records", () => {
    expect(audioPlan(c)).toBe("16 tracks, 24-bit WAV");
    expect(audioPlan({ ...c, tracks: "5" })).toBe("1 track, 24-bit WAV");
    expect(audioPlan({ ...c, tracks: "" })).toBe("32 tracks, 24-bit WAV");
  });
  it("is empty when no audio is recorded", () => {
    expect(audioPlan({ ...c, enabled: false })).toBe("");
    expect(audioPlan(undefined)).toBe("");
    expect(audioPlan({ ...c, tracks: "40" })).toBe("");
  });
  it("counts", () => {
    expect([plural(0, "marker"), plural(1, "marker"), plural(2, "marker")]).toEqual(["0 markers", "1 marker", "2 markers"]);
  });
});

describe("audio trouble", () => {
  const info: AudioInfo = { rate: 48000, bits: 24, channels: 32, tracks: [{ channel: 1 }, { channel: 2 }], offset: 0, frames: 0 };
  const st = (patch: Partial<AudioStatus>): AudioStatus => ({
    ...info,
    state: "recording",
    seconds: 0,
    bytes: 0,
    byte_rate: 0,
    free: -1,
    remaining: -1,
    ...patch,
  });
  it("says when the audio is not recorded, or in trouble", () => {
    expect(audioTrouble(undefined)).toBeNull();
    expect(audioTrouble(st({}))).toBeNull();
    expect(audioTrouble(st({ state: "failed", error: "Channels count non available" }))).toEqual({
      level: "failed",
      text: "Channels count non available",
    });
    expect(audioTrouble(st({ state: "failed" }))?.text).toBe("the audio stopped");
    expect(audioTrouble(st({ warnings: ["not the console's card", "no audio for 5s"] }))).toEqual({
      level: "warn",
      text: "not the console's card · no audio for 5s",
    });
  });
  it("sums up the audio of a session", () => {
    expect(audioSummary(undefined)).toBe("no audio");
    expect(audioSummary(info)).toBe("audio: 2 tracks");
    expect(audioSummary({ ...info, error: "the recorder stopped" })).toBe("audio failed");
    expect(audioSummary({ ...info, frames: 48000, error: "the recorder stopped" })).toBe("audio: 2 tracks, cut short");
  });
});

describe("saving the audio settings", () => {
  const saved: AudioConfig = {
    enabled: true,
    recorder: "arecord",
    device: "hw:CARD=PCH,DEV=0",
    channels: 32,
    rate: 48000,
    bits: 24,
    tracks: "1-16,23-24",
  };
  const pch: AudioDevice = { recorder: "arecord", id: "hw:CARD=PCH,DEV=0", name: "HDA Intel PCH, ALC294 Analog", channels: 2 };
  const dn32: AudioDevice = { recorder: "arecord", id: "hw:CARD=DN32USB,DEV=0", name: "DN32-USB, USB Audio", channels: 32, console: true };

  it("refuses more channels than the device has", () => {
    // the concert of 2026-10-25: 32 channels from the laptop's card
    expect(tooManyChannels(saved, [pch, dn32])).toBe(
      "HDA Intel PCH, ALC294 Analog has 2 input channels, not 32: set Channels of the device to 2, or choose the console's card (★ DN32-USB, USB Audio).",
    );
    expect(tooManyChannels({ ...saved, channels: 2 }, [pch, dn32])).toBe("");
    expect(tooManyChannels({ ...saved, device: dn32.id }, [pch, dn32])).toBe("");
    expect(tooManyChannels({ ...saved, channels: 8 }, [{ ...pch, channels: 4, min_channels: 2 }])).toBe(
      "HDA Intel PCH, ALC294 Analog has 2 to 4 input channels, not 8: set Channels of the device to 4.",
    );
    // unknown channels, a device not found, or no list yet
    expect(tooManyChannels(saved, [{ ...pch, channels: undefined }])).toBe("");
    expect(tooManyChannels({ ...saved, device: "hw:CARD=XUSB,DEV=0" }, [pch])).toBe("");
    expect(tooManyChannels({ ...saved, recorder: "parecord" }, [pch])).toBe("");
    expect(tooManyChannels(saved, null)).toBe("");
  });

  it("lists what changed", () => {
    expect(configChanges(saved, saved)).toEqual([]);
    expect(configChanges(saved, { ...saved, device: dn32.id, tracks: "" })).toEqual([
      { label: "Device", from: "hw:CARD=PCH,DEV=0", to: "hw:CARD=DN32USB,DEV=0" },
      { label: "Tracks", from: "1-16,23-24", to: "all" },
    ]);
    expect(configChanges(saved, { ...saved, enabled: false, rate: 44100, plain_names: true })).toEqual([
      { label: "Audio tracks", from: "recorded", to: "off" },
      { label: "Rate", from: "48 kHz", to: "44.1 kHz" },
      { label: "File names", from: "from the routing", to: "Track 01…" },
    ]);
  });

  it("takes the empty fields the server leaves out for the same settings", () => {
    const fromServer: AudioConfig = { enabled: true, recorder: "arecord", device: "hw:CARD=PCH,DEV=0", channels: 32, rate: 48000, bits: 24 };
    // "All" tracks picked on the page, and the fields the forms fill in
    expect(sameConfig(fromServer, { ...fromServer, tracks: "", plain_names: false, command: "", input: "" })).toBe(true);
    expect(sameConfig(fromServer, { ...fromServer, tracks: "1-2" })).toBe(false);
  });
});
