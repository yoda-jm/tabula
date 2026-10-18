import { describe, expect, it } from "vitest";
import type { Lookup } from "./routing";
import {
  auxBlockSource,
  blockLabel,
  blockSource,
  channelsOnPort,
  describeSource,
  haIndexAddr,
  headampOfPort,
  headampPort,
  inputSource,
  outputDestinations,
  outputSource,
  portById,
  portChannelSource,
  portLabel,
  stripsUsingSource,
  userSource,
} from "./routing";

/** A Lookup over the demo scene's routing: DL32 on AES50 A, recording card. */
function demo(extra: Record<string, number | string> = {}): Lookup {
  const v: Record<string, number | string> = {
    "/config/routing/routswitch": 0,
    "/config/routing/IN/1-8": "A1-8",
    "/config/routing/IN/9-16": "A9-16",
    "/config/routing/IN/17-24": "A17-24",
    "/config/routing/IN/25-32": "A25-32",
    "/config/routing/PLAY/1-8": "CARD1-8",
    "/config/routing/PLAY/9-16": "CARD9-16",
    "/config/routing/PLAY/17-24": "CARD17-24",
    "/config/routing/PLAY/25-32": "CARD25-32",
    "/config/routing/AES50A/1-8": "OUT1-8",
    "/config/routing/AES50A/9-16": "OUT9-16",
    "/config/routing/AES50A/17-24": "OUT1-8",
    "/config/routing/AES50A/25-32": "OUT9-16",
    "/config/routing/AES50A/33-40": "P161-8",
    "/config/routing/AES50A/41-48": "P169-16",
    "/config/routing/CARD/1-8": "A1-8",
    "/config/routing/CARD/9-16": "A9-16",
    "/config/routing/CARD/17-24": "OUT9-16",
    "/config/routing/CARD/25-32": "A25-32",
    "/config/routing/OUT/1-4": "OUT1-4",
    "/config/routing/OUT/5-8": "OUT5-8",
    "/config/routing/OUT/9-12": "OUT9-12",
    "/config/routing/OUT/13-16": "OUT13-16",
    "/outputs/main/01/src": 4,
    "/outputs/main/13/src": 22,
    "/bus/01/config/name": "IEM Theo",
    "/mtx/03/config/name": "Rappel",
    "/ch/12/config/name": "Vx Theo",
    "/ch/12/config/source": 12,
    "/ch/17/config/name": "Theo",
    "/ch/17/config/source": 12,
    "/ch/01/config/source": 1,
    "/ch/01/config/name": "Hang",
    ...extra,
  };
  return { text: (a) => (a in v ? String(v[a]) : undefined), value: (a) => v[a] };
}

describe("routing blocks", () => {
  it("reads each block of the schema's enums", () => {
    expect(blockSource("A9-16", 3)).toEqual({ kind: "aes50a", n: 12 });
    expect(blockSource("AN1-8", 0)).toEqual({ kind: "local", n: 1 });
    expect(blockSource("B41-48", 7)).toEqual({ kind: "aes50b", n: 48 });
    expect(blockSource("CARD25-32", 1)).toEqual({ kind: "card", n: 26 });
    expect(blockSource("UIN9-16", 0)).toEqual({ kind: "userin", n: 9 });
    expect(blockSource("UOUT41-48", 7)).toEqual({ kind: "userout", n: 48 });
    expect(blockSource("OUT9-16", 0)).toEqual({ kind: "output", n: 9 });
    expect(blockSource("P169-16", 2)).toEqual({ kind: "p16", n: 11 });
    expect(blockSource("P1613-16", 3)).toEqual({ kind: "p16", n: 16 });
    expect(blockSource("OUT5-8", 0)).toEqual({ kind: "output", n: 5 });
    expect(blockSource("OUT1-4", 4)).toBeNull(); // past the block
    expect(blockSource("AUX1-6/Mon", 5)).toEqual({ kind: "auxout", n: 6 });
    expect(blockSource("AUX1-6/Mon", 7)).toEqual({ kind: "monitor", n: 2 });
    expect(blockSource("AUX/CR", 1, 1)).toEqual({ kind: "auxout", n: 6 });
    expect(blockSource("AUX/CR", 2, 1)).toEqual({ kind: "monitor", n: 1 });
    expect(blockSource("AuxIN1-6/TB", 6)).toEqual({ kind: "talkback", n: 1 });
    expect(blockSource("???", 0)).toBeNull();
  });

  it("describes blocks", () => {
    expect(blockLabel("A17-24")).toBe("AES50 A 17-24");
    expect(blockLabel("P161-8")).toBe("P16 1-8");
    expect(blockLabel("UOUT1-8")).toBe("User out 1-8");
    expect(blockLabel("AUX1-6/Mon")).toBe("Aux out 1-6, monitor L/R");
    expect(blockLabel("AUX1-4")).toBe("Local aux in 1-6");
  });

  it("routes the aux inputs", () => {
    expect(auxBlockSource("AUX1-4", 5)).toEqual({ kind: "auxin", n: 5 });
    expect(auxBlockSource("A1-4", 2)).toEqual({ kind: "aes50a", n: 2 });
    expect(auxBlockSource("A1-4", 5)).toEqual({ kind: "auxin", n: 5 });
    expect(auxBlockSource("CARD1-6", 6)).toEqual({ kind: "card", n: 6 });
  });
});

describe("inputs and headamps", () => {
  it("follows the input routing, and the playback routing when switched", () => {
    expect(inputSource(demo(), 12)).toEqual({ kind: "aes50a", n: 12 });
    expect(inputSource(demo({ "/config/routing/routswitch": 1 }), 12)).toEqual({ kind: "card", n: 12 });
  });

  it("numbers the headamps", () => {
    expect(haIndexAddr("/ch/01")).toBe("/-ha/00/index");
    expect(haIndexAddr("/ch/32")).toBe("/-ha/31/index");
    expect(haIndexAddr("/auxin/08")).toBe("/-ha/39/index");
    expect(haIndexAddr("/bus/01")).toBeUndefined();
    expect(headampPort(-1)).toBeNull();
    expect(portLabel(headampPort(5)!)).toBe("Local 6");
    expect(portLabel(headampPort(43)!)).toBe("AES50 A 12");
    expect(portLabel(headampPort(127)!)).toBe("AES50 B 48");
    for (const i of [0, 31, 32, 79, 80, 127]) expect(headampOfPort(headampPort(i)!)).toBe(i);
    expect(headampOfPort({ kind: "card", n: 1 })).toBe(-1);
  });

  it("names the channels an input feeds", () => {
    expect(channelsOnPort(demo(), { kind: "aes50a", n: 12 })).toEqual(["Ch 12 Vx Theo", "Ch 17 Theo"]);
    expect(channelsOnPort(demo(), { kind: "aes50a", n: 30 })).toEqual([]);
    expect(stripsUsingSource(demo({ "/auxin/01/config/source": 33, "/auxin/01/config/name": "BT" }), 33)).toEqual(["Aux 1 BT"]);
  });
});

describe("outputs", () => {
  it("names output sources", () => {
    expect(outputSource(0).label).toBe("OFF");
    expect(outputSource(1)).toEqual({ label: "Main L", strip: "/main/st" });
    expect(outputSource(4)).toEqual({ label: "MixBus 01", strip: "/bus/01" });
    expect(outputSource(19)).toEqual({ label: "MixBus 16", strip: "/bus/16" });
    expect(outputSource(22)).toEqual({ label: "Matrix 3", strip: "/mtx/03" });
    expect(outputSource(26)).toEqual({ label: "Ch 01 direct", strip: "/ch/01" });
    expect(outputSource(57)).toEqual({ label: "Ch 32 direct", strip: "/ch/32" });
    expect(outputSource(58)).toEqual({ label: "Aux 1 direct", strip: "/auxin/01" });
    expect(outputSource(66)).toEqual({ label: "FX 1L direct", strip: "/fxrtn/01" });
    expect(outputSource(73)).toEqual({ label: "FX 4R direct", strip: "/fxrtn/08" });
    expect(outputSource(76).label).toBe("Talkback");
  });

  it("names user routing values", () => {
    expect(userSource(0)).toBeNull();
    expect(userSource(33)).toEqual({ kind: "aes50a", n: 1 });
    expect(userSource(160)).toEqual({ kind: "card", n: 32 });
    expect(userSource(184)).toEqual({ kind: "output", n: 16 });
    expect(userSource(208)).toEqual({ kind: "monitor", n: 2 });
  });

  it("follows a port channel to the strip it carries", () => {
    const l = demo();
    const aes = portById("AES50A");
    expect(portChannelSource(l, aes, 1)).toEqual({ kind: "output", n: 1 });
    expect(portChannelSource(l, aes, 34)).toEqual({ kind: "p16", n: 2 });
    expect(describeSource(l, portChannelSource(l, aes, 1))).toBe("Output 1 · MixBus 01 IEM Theo");
    expect(describeSource(l, portChannelSource(l, aes, 13))).toBe("Output 13 · Matrix 3 Rappel");
    expect(describeSource(l, portChannelSource(l, portById("CARD"), 12))).toBe("AES50 A 12 · Ch 12 Vx Theo, Ch 17 Theo");
    expect(describeSource(l, null)).toBe("OFF");
  });

  it("lists where each output goes", () => {
    const d = outputDestinations(demo());
    expect(d[0]).toEqual(["AES50 A 1", "AES50 A 17", "XLR out 1"]); // AES50 B is not patched here
    expect(d[8]).toContain("Card 17");
  });
});
