import { describe, expect, it } from "vitest";
import { OSC_DESTS, SOLO_ADDRS, TALK_DESTS, groupsOf, silencedBy, soloAddr } from "./monitor";
import { stripByPath } from "./strips";

describe("monitor", () => {
  it("numbers the solo switches as the console does", () => {
    const at = (path: string) => soloAddr(stripByPath(path)!);
    expect(at("/ch/01")).toBe("/-stat/solosw/01");
    expect(at("/auxin/01")).toBe("/-stat/solosw/33");
    expect(at("/fxrtn/08")).toBe("/-stat/solosw/48");
    expect(at("/bus/16")).toBe("/-stat/solosw/64");
    expect(at("/mtx/06")).toBe("/-stat/solosw/70");
    expect(at("/main/st")).toBe("/-stat/solosw/71");
    expect(at("/main/m")).toBe("/-stat/solosw/72");
    expect(at("/dca/8")).toBe("/-stat/solosw/80");
    expect(SOLO_ADDRS).toHaveLength(80);
  });

  it("names the destinations", () => {
    expect(TALK_DESTS).toHaveLength(18);
    expect(OSC_DESTS).toHaveLength(26);
    expect(OSC_DESTS[18]).toBe("L+R");
    expect(OSC_DESTS[25]).toBe("Mtx 6");
  });

  it("tells what silences a strip", () => {
    expect(groupsOf(0b100101, 6)).toEqual([1, 3, 6]);
    const groupOn = (n: number) => n === 3;
    const dcaOn = (n: number) => n !== 2;
    expect(silencedBy({ mute: 0b000101, dca: 0b0110 }, groupOn, dcaOn)).toEqual(["mute group 3", "DCA 2"]);
    expect(silencedBy({ mute: 0b000001, dca: 0b0100 }, groupOn, dcaOn)).toEqual([]);
  });
});
