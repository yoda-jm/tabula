import { describe as group, expect, it } from "vitest";
import { describe, firstFree, kindInfo, readSlots, slotKey, slotLabel, slotPath } from "./library";
import type { Lookup } from "./routing";

function lookup(v: Record<string, number | string>): Lookup {
  return { text: (a) => (a in v ? String(v[a]) : undefined), value: (a) => v[a] };
}

group("library", () => {
  it("numbers slots as the console does", () => {
    const scene = kindInfo("scene");
    const ch = kindInfo("libchan");
    expect(slotPath(scene, 0)).toBe("/-show/showfile/scene/000");
    expect(slotPath(scene, 99)).toBe("/-show/showfile/scene/099");
    expect(slotPath(ch, 0)).toBe("/-libs/ch/001");
    expect(slotPath(kindInfo("libmon"), 99)).toBe("/-libs/mon/100");
    expect(slotLabel(scene, 3)).toBe("03");
    expect(slotLabel(ch, 45)).toBe("46");
  });

  it("reads the listings", () => {
    const l = lookup({
      "/-show/showfile/scene/000/name": "Soundcheck",
      "/-show/showfile/scene/000/notes": "line check",
      "/-show/showfile/scene/000/hasdata": 1,
      "/-show/showfile/scene/001/hasdata": 0,
      "/-libs/ch/046/name": "Vox",
      "/-libs/ch/046/hasdata": 1,
    });
    const scenes = readSlots(l, kindInfo("scene"));
    expect(scenes).toHaveLength(100);
    expect(scenes[0]).toEqual({ slot: 0, name: "Soundcheck", note: "line check", used: true });
    expect(firstFree(scenes)).toBe(1);
    const presets = readSlots(l, kindInfo("libchan"));
    expect(presets[45]).toEqual({ slot: 45, name: "Vox", note: "", used: true });
    expect(presets.filter((s) => s.used)).toHaveLength(1);
  });

  it("says what a request does", () => {
    expect(describe({ op: "load", kind: "scene", index: 3 }, "Concert")).toBe("Recall scene 03 “Concert”");
    expect(describe({ op: "load", kind: "libfx", index: 0 }, "Hall")).toBe("Load effect preset 1 “Hall”");
    expect(describe({ op: "copy", kind: "scene", index: 3, to: 4 }, "")).toBe("Copy scene 03 to 04");
    expect(describe({ op: "rename", kind: "snippet", index: 0, name: "Intro" }, "Old")).toBe(
      "Rename snippet 00 “Old” to “Intro”",
    );
  });

  it("tells the slots of two libraries apart", () => {
    expect(slotKey(kindInfo("scene"), 3)).not.toBe(slotKey(kindInfo("snippet"), 3));
    expect(slotKey(kindInfo("scene"), 3)).toBe(slotKey(kindInfo("scene"), 3));
  });
});
