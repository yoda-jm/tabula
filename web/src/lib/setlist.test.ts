import { describe, expect, it } from "vitest";
import { nextSong, parseSetlist, songLabel, songTimes } from "./setlist";

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
