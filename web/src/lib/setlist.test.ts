import { describe, expect, it } from "vitest";
import { nextSong, parseSetlist, parseSongs, setlistText, songLabel, songTempo, songTimes } from "./setlist";

describe("setlist", () => {
  it("reads songs typed one per line", () => {
    expect(parseSetlist("  Opening \n\n3. Second   song\n04 - Third\n5) Fourth\r\n7:Encore\n1979\n2:00 AM")).toEqual([
      "Opening",
      "Second song",
      "Third",
      "Fourth",
      "Encore",
      "1979",
      "2:00 AM",
    ]);
    expect(parseSetlist(" \n ")).toEqual([]);
  });

  it("reads the tempo typed after a song", () => {
    expect(parseSongs("1. Opening 120 bpm\nBallad · 72.5 BPM\nEncore (96 bpm)\nShuffle-140bpm\nNo tempo\n128 bpm")).toEqual({
      songs: ["Opening", "Ballad", "Encore", "Shuffle", "No tempo", "128 bpm"],
      tempos: [120, 72.5, 96, 140, 0, 0],
    });
    // only a tempo set apart, from 20 to 300: the words stay in the name otherwise
    expect(parseSongs("Song 12 bpm\nSong 1120 bpm\nSong 120.25 bpm\nSong 99.5 bpm").songs).toEqual([
      "Song 12 bpm",
      "Song 1120 bpm",
      "Song 120.25 bpm",
      "Song",
    ]);
    const s = { songs: ["Opening", "Ballad"], tempos: [120, 0], next: 0 };
    expect(setlistText(s)).toBe("Opening · 120 BPM\nBallad");
    expect(parseSongs(setlistText(s))).toEqual({ songs: s.songs, tempos: s.tempos });
    expect(songTempo(s, 0)).toBe(120);
    expect(songTempo(s, 5)).toBe(0);
    expect(songTempo({ songs: ["A"], next: 0 }, 0)).toBe(0);
  });

  it("labels and finds the songs", () => {
    expect(songLabel(2, "Third")).toBe("3. Third");
    expect(nextSong({ songs: ["A", "B"], next: 1 })).toBe("2. B");
    expect(nextSong({ songs: ["A", "B"], next: 2 })).toBeNull();
    expect(nextSong(null)).toBeNull();
    const markers = [
      { t: 10, marker: "1. A" },
      { t: 95, marker: "Break" },
      { t: 130, marker: "2. C" }, // reordered after it was marked
    ];
    expect(songTimes(["A", "B", "C"], markers)).toEqual([10, undefined, 130]);
  });
});
