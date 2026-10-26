import { describe, expect, it } from "vitest";
import { headerOf, looksLikeScene, sceneFileName } from "./scenefiles";

describe("scene files", () => {
  it("names the file as the server does", () => {
    expect(sceneFileName("GVO 2026")).toBe("GVO 2026.scn");
    expect(sceneFileName("a/b")).toBe("a-b.scn");
    expect(sceneFileName("..")).toBe("scene.scn");
    expect(sceneFileName(" Set: 1? ")).toBe("Set- 1-.scn");
  });

  it("reads the header", () => {
    expect(headerOf('#4.0# "GVO-2026" "Rappel 10 m" %000000000 1     \n/ch/01/config "x" 1 RD 1')).toEqual({
      name: "GVO-2026",
      note: "Rappel 10 m",
    });
    expect(headerOf('#2.1# "Old" %000000000 1')).toEqual({ name: "Old", note: "" });
    expect(headerOf('#4.0# "say "hi"" ""')).toEqual({ name: 'say "hi"', note: "" });
    expect(headerOf('#4.0# "a\\b" "it\'s"')).toEqual({ name: "a\\b", note: "it's" });
    expect(headerOf("/ch/01/config")).toBeNull();
  });

  it("tells a scene from another file", () => {
    expect(looksLikeScene('#4.0# "GVO" "" %000000000 1\n/ch/01/config "Hang" 1 YE 33\n')).toBe(true);
    expect(looksLikeScene("RIFF....WAVEfmt ")).toBe(false);
    expect(looksLikeScene('#4.0# "GVO" ""\n')).toBe(false);
  });
});
