import { describe, expect, it } from "vitest";
import type { Lookup } from "./routing";
import { CHANNEL_SAFES, PARAM_SAFES, compact, isSet, sceneKeeps, snippetChanges, snippetRows, ticked } from "./safes";

const lookupOf = (values: Record<string, unknown>): Lookup => ({ text: () => undefined, value: (a) => values[a] });

describe("safes", () => {
  it("reads bit 31 of a map sent as a negative int", () => {
    expect(isSet(-2147483648, 31)).toBe(true);
    expect(isSet(-2147483648, 0)).toBe(false);
    expect(isSet(0x24, 2)).toBe(true);
    expect(isSet(0x24, 5)).toBe(true);
  });

  it("names the bits as Maillot's document does", () => {
    // 0x24: EQ and Groups are safe (the document's example)
    expect(ticked(0x24, PARAM_SAFES[0].bits)).toEqual(["EQ", "Groups"]);
    expect(ticked(0x8001, PARAM_SAFES[1].bits)).toEqual(["Mix 1", "Mix 16"]);
    const ret = CHANNEL_SAFES.find((r) => r.addr.endsWith("/return"))!;
    expect(ticked(0x0181, ret.bits)).toEqual(["Aux 1", "Aux 8", "FX 1L"]);
    expect(ret.bits[15]).toEqual({ bit: 15, label: "FX 4R", path: "/fxrtn/08" });
    // DCA 8 is bit 15 of a snippet's maingrps (32768 in the document)
    const main = snippetRows(0).strips[2];
    expect(ticked(32768, main.bits)).toEqual(["DCA 8"]);
  });

  it("compacts numbered labels", () => {
    expect(compact(["Mix 1", "Mix 2", "Mix 3", "Mix 5", "L/R", "DCA 1", "DCA 2"])).toBe("Mix 1-3, 5, L/R, DCA 1-2");
    expect(compact(["Ch 1", "Ch 2", "Ch 17", "FX 1L", "FX 1R"])).toBe("Ch 1-2, 17, FX 1L, FX 1R");
    expect(compact([])).toBe("");
  });

  it("says what a scene recall keeps", () => {
    const l = lookupOf({
      "/-show/showfile/scene/003/safes": 0x106, // Routing I/O, Talkback and Effects (the document's example)
      "/-show/showfile/show/inputs": 0x80,
      "/-show/showfile/show/chan16": 0x8003,
      "/-show/showfile/show/chan32": 1,
      "/-show/showfile/show/lrmtxdca": 0x140,
      "/-prefs/safe_masterlevels": 1,
    });
    expect(sceneKeeps(l, 3)).toEqual([
      "This scene's safes: Talkback, Effects, Routing",
      "Input channels: Mute",
      "Whole strips: Ch 1-2, 16-17, L/R, DCA 1",
      "Main levels (L/R and M/C faders)",
    ]);
    // scene 00 has no safes of its own
    expect(sceneKeeps(l, 0)[0]).toBe("Input channels: Mute");
    expect(sceneKeeps(lookupOf({}), 3)).toEqual([]);
  });

  it("says what a snippet recall changes", () => {
    const at = (f: string) => `/-show/showfile/snippet/007/${f}`;
    const l = lookupOf({
      [at("eventtyp")]: (1 << 6) | (1 << 7) | (1 << 14) | (1 << 23),
      [at("channels")]: 0xff,
      [at("auxbuses")]: 1 << 16,
      [at("maingrps")]: 1 << 8,
    });
    expect(snippetChanges(l, 7)).toBe("Fader & pan, Mute of Ch 1-8, Mix 1, DCA 1; FX 2; the console's routing");
    // parameters without strips: nothing of the strips
    expect(snippetChanges(lookupOf({ [at("eventtyp")]: 1 << 2 }), 7)).toBe("");
  });
});
