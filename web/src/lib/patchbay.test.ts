import { describe, expect, it } from "vitest";
import type { PathTree } from "./patchbay";
import { buildPatchbay, defaultPorts, layout, localIO, pathTree, ranges, related, visibleNodes } from "./patchbay";
import type { Lookup } from "./routing";

type Entry = number | string | [number, string]; // [raw value, node text]

/** A Lookup over the demo scene's routing: DL32 on AES50 A, IEM buses, FX on buses 13-16, card tracks. */
function demo(extra: Record<string, Entry> = {}): Lookup {
  const v: Record<string, Entry> = {
    "/config/routing/routswitch": 0,
    "/config/routing/IN/1-8": "A1-8",
    "/config/routing/IN/9-16": "A9-16",
    "/config/routing/IN/17-24": "A17-24",
    "/config/routing/IN/25-32": "A25-32",
    "/config/routing/IN/AUX": "AUX1-4",
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
    "/ch/12/config/name": "Vx Theo",
    "/ch/12/config/source": 12,
    "/ch/12/mix/on": 1,
    "/ch/12/mix/st": 1,
    "/ch/12/mix/13/on": 1,
    "/ch/12/mix/13/level": [0.5, "-10.0"],
    "/ch/12/mix/13/type": "POST",
    "/ch/17/config/name": "IEM Theo",
    "/ch/17/config/source": 12,
    "/ch/17/mix/on": 1,
    "/ch/17/mix/01/on": 1,
    "/ch/17/mix/01/level": [0.75, "0.0"],
    "/ch/17/mix/01/type": "PRE",
    "/ch/17/mix/02/on": 1,
    "/ch/17/mix/02/level": [0, "-oo"], // −∞: no path
    "/ch/01/config/name": "Hang",
    "/ch/01/config/source": 1,
    "/ch/01/mix/on": 0, // muted
    "/ch/01/mix/st": 1,
    "/headamp/043/gain": "+32.0",
    "/headamp/043/phantom": 1,
    "/bus/01/config/name": "Theo IEM",
    "/bus/01/mix/on": 1,
    "/bus/13/config/name": "FX Room",
    "/bus/13/mix/on": 1,
    "/fx/1/source/l": "MIX13",
    "/fx/1/source/r": "MIX13",
    "/fx/2/source/l": "INS",
    "/fx/2/source/r": "INS",
    "/fxrtn/01/mix/on": 1,
    "/fxrtn/01/mix/st": 1,
    "/fxrtn/02/mix/on": 1,
    "/fxrtn/02/mix/st": 1,
    "/main/st/mix/on": 1,
    "/main/st/mix/01/on": 1,
    "/main/st/mix/01/level": [0.75, "0.0"],
    "/main/st/mix/01/type": "POST",
    "/mtx/01/config/name": "FacadeL",
    "/outputs/main/01/src": 4, // MixBus 01
    "/outputs/main/01/pos": "POST",
    "/outputs/main/09/src": 0, // OFF: card track 17 carries nothing
    "/outputs/main/13/src": 20, // Matrix 1
    "/outputs/main/13/pos": "POST",
    "/outputs/main/13/delay/on": 1,
    "/outputs/main/13/delay/time": "29.2",
    ...extra,
  };
  return {
    text: (a) => (a in v ? String(Array.isArray(v[a]) ? (v[a] as [number, string])[1] : v[a]) : undefined),
    value: (a) => (a in v ? (Array.isArray(v[a]) ? (v[a] as [number, string])[0] : (v[a] as number | string)) : undefined),
  };
}

const linkOf = (pb: ReturnType<typeof buildPatchbay>, from: string, to: string) =>
  pb.links.find((k) => k.from === from && k.to === to);

/** chains flattens a path tree into its paths, as label strings: "Ch 17 > Bus 01 > Out 01 > A 1". */
function chains(t: PathTree, prefix: string[] = []): string[] {
  const here = [...prefix, t.node.label];
  return t.children.length ? t.children.flatMap((c) => chains(c, here)) : [here.join(" > ")];
}

describe("patchbay", () => {
  it("feeds channels from the stage box through the input routing, with the headamp", () => {
    const pb = buildPatchbay(demo(), { model: "M32C" });
    const a12 = pb.nodes.get("in:aes50a:12")!;
    expect(a12).toMatchObject({ col: 0, label: "A 12", socket: "box in 12", detail: "+32.0 dB", phantom: true });
    expect(linkOf(pb, "in:aes50a:12", "/ch/12")).toMatchObject({ kind: "patch", label: "In 12" });
    expect(linkOf(pb, "in:aes50a:12", "/ch/17")).toMatchObject({ label: "In 12" });
    expect(pb.nodes.get("/ch/12")).toMatchObject({ label: "Ch 12", name: "Vx Theo", muted: false });
  });

  it("follows sends above −∞ and assigns, with their tap", () => {
    const pb = buildPatchbay(demo());
    expect(linkOf(pb, "/ch/17", "/bus/01")).toMatchObject({ kind: "send", label: "PRE 0.0" });
    expect(linkOf(pb, "/ch/17", "/bus/02")).toBeUndefined(); // −∞
    expect(linkOf(pb, "/ch/12", "/main/st")).toMatchObject({ label: "LR" });
    expect(linkOf(pb, "/ch/12", "/bus/13")).toMatchObject({ label: "POST -10.0" });
    expect(linkOf(pb, "/main/st", "/mtx/01")).toMatchObject({ label: "POST 0.0" });
    expect(linkOf(pb, "/ch/01", "/main/st")).toMatchObject({ muted: true }); // a muted channel
  });

  it("brings effects back on their FX returns, not those used as inserts", () => {
    const pb = buildPatchbay(demo());
    expect(linkOf(pb, "/bus/13", "/fxrtn/01")).toMatchObject({ kind: "fx", label: "FX 1" });
    expect(linkOf(pb, "/bus/13", "/fxrtn/02")).toBeDefined();
    expect(linkOf(pb, "/fxrtn/01", "/main/st")).toBeDefined();
    // FX 2 is an insert: its returns carry nothing, their assigns are no path
    expect(linkOf(pb, "/fxrtn/03", "/main/st")).toBeUndefined();
  });

  it("follows outputs to the ports and says what each port channel carries", () => {
    const pb = buildPatchbay(demo(), { model: "M32C" });
    expect(linkOf(pb, "/bus/01", "/outputs/main/01")).toMatchObject({ label: "POST" });
    expect(linkOf(pb, "/mtx/01", "/outputs/main/13")).toMatchObject({ label: "POST · 29.2 ms" });
    expect(pb.nodes.get("/outputs/main/13")).toMatchObject({ name: "FacadeL", detail: "29.2 ms" });
    expect(pb.nodes.get("AES50A:1")).toMatchObject({ label: "A 1", socket: "box out 1", name: "Theo IEM", detail: "Out 01 · Theo IEM" });
    expect(pb.nodes.get("AES50A:17")).toMatchObject({ socket: undefined, name: "Theo IEM" });
    expect(pb.nodes.get("CARD:12")).toMatchObject({ label: "Track 12", name: "Vx Theo", detail: "A 12 · Ch 12 Vx Theo" });
    // an output without a source carries nothing, nor does the card track taking it
    expect(linkOf(pb, "/outputs/main/09", "CARD:17")).toBeUndefined();
    // P16 outputs are OFF: AES50 A 33-48 carry nothing
    expect(pb.links.some((k) => k.to === "AES50A:33")).toBe(false);
  });

  it("knows which local sockets each model has", () => {
    expect(localIO("M32C")).toEqual({ ins: 0, outs: 0, aux: 0 });
    expect(localIO("m32r")).toEqual({ ins: 16, outs: 8, aux: 6 });
    expect(localIO("unknown")).toEqual({ ins: 32, outs: 16, aux: 6 });
    const aux = { "/auxin/01/config/source": 33, "/auxin/01/mix/on": 1, "/auxin/01/mix/st": 1 };
    const core = buildPatchbay(demo(aux), { model: "M32C" });
    expect(core.nodes.has("OUT:1")).toBe(false);
    expect(core.nodes.has("in:local:1")).toBe(false);
    expect(linkOf(core, "/auxin/01", "/main/st")).toBeUndefined(); // no aux in socket on an M32C
    const full = buildPatchbay(demo(aux), { model: "M32" });
    expect(full.nodes.get("OUT:1")).toMatchObject({ label: "XLR 1", detail: "Out 01 · Theo IEM" });
    expect(linkOf(full, "in:auxin:1", "/auxin/01")).toMatchObject({ label: "Aux 1" });
  });

  it("warns about local inputs the model does not have", () => {
    const pb = buildPatchbay(demo({ "/config/routing/IN/1-8": "AN1-8" }), { model: "M32C" });
    expect(pb.nodes.get("in:local:1")).toMatchObject({ missing: true });
    expect(pb.warnings.map((w) => w.text)).toContain(
      "Local inputs 1-8 are routed, but an M32C has none: nothing comes from them.",
    );
  });

  it("warns when the inputs come from the playback routing", () => {
    const pb = buildPatchbay(demo({ "/config/routing/routswitch": 1 }));
    expect(linkOf(pb, "in:card:12", "/ch/12")).toBeDefined();
    expect(pb.warnings[0].text).toMatch(/playback routing \(PLAY\)/);
  });

  it("warns about recorded card tracks that carry nothing, 48 V feeding nothing and mixes going nowhere", () => {
    const pb = buildPatchbay(
      demo({
        "/headamp/060/phantom": 1, // A 29: no channel uses it, but card track 29 records it
        "/headamp/090/phantom": 1, // B 11: nothing
        "/ch/12/mix/06/on": 1,
        "/ch/12/mix/06/level": [0.5, "-10.0"],
      }),
      { model: "M32C", recorded: [1, 2, 3, 17, 18, 20] },
    );
    const texts = pb.warnings.map((w) => w.text);
    expect(texts).toContain("Card tracks 17-18, 20 are recorded but carry nothing.");
    expect(texts).toContain("48 V is on for B 11 (box in 11), which feeds nothing.");
    expect(texts.some((t) => t.startsWith("48 V is on for A 29"))).toBe(false);
    expect(texts).toContain("Bus 06 gets a signal but goes nowhere (no output, matrix or effect).");
    expect(pb.nodes.get("CARD:17")).toMatchObject({ recorded: true });
  });

  it("follows the user routing", () => {
    const pb = buildPatchbay(demo({ "/config/routing/IN/9-16": "UIN1-8", "/config/userrout/in/04": 50 })); // AES50 A 18
    expect(linkOf(pb, "in:aes50a:18", "/ch/12")).toMatchObject({ label: "In 12 · user in 4" });
  });

  it("marks idle mixes and lists what is on a path", () => {
    const pb = buildPatchbay(demo(), { model: "M32C" });
    expect(pb.nodes.get("/bus/01")!.idle).toBeUndefined();
    expect(pb.nodes.get("/bus/05")!.idle).toBe(true);
    const shown = visibleNodes(pb, { all: false }).map((n) => n.id);
    expect(shown).toContain("in:aes50a:12");
    expect(shown).not.toContain("in:aes50b:1"); // nothing uses it
    expect(shown).not.toContain("/ch/30"); // no source path
    expect(shown).toContain("AES50A:13");
    expect(shown).not.toContain("AES50A:17"); // past a stage box's 16 outputs
    expect(shown.indexOf("in:aes50a:12")).toBeLessThan(shown.indexOf("/ch/12"));
    expect(visibleNodes(pb, { all: true }).length).toBe(pb.nodes.size);
    // AES50 B only when a stage box is there
    expect([...defaultPorts(pb)].sort()).toEqual(["AES50A", "CARD", "OUT"]);
    const ports = defaultPorts(pb);
    expect(visibleNodes(pb, { all: false, ports }).some((n) => n.port === "AES50B")).toBe(false);
    const withB = buildPatchbay(demo({ "/config/routing/IN/25-32": "B1-8", "/ch/25/config/source": 25 }), { model: "M32C" });
    expect(defaultPorts(withB).has("AES50B")).toBe(true);
  });

  it("finds everything on a node's paths", () => {
    const pb = buildPatchbay(demo(), { model: "M32C" });
    const r = related(pb, "/ch/17");
    expect([...r.nodes]).toEqual(expect.arrayContaining(["in:aes50a:12", "/bus/01", "/outputs/main/01", "AES50A:1", "AES50A:17"]));
    expect(r.nodes.has("/ch/12")).toBe(false); // a sibling on the same socket, not on ch 17's paths
    const down = chains(pathTree(pb, "/ch/17", "down")!);
    expect(down).toEqual(["Ch 17 > Bus 01 > Out 01 > A 1", "Ch 17 > Bus 01 > Out 01 > A 17"]);
    const up = chains(pathTree(pb, "AES50A:13", "up")!);
    expect(up).toContain("A 13 > Out 13 > Mtx 1 > LR > Ch 12 > A 12");
    expect(up).toContain("A 13 > Out 13 > Mtx 1 > LR > FX 1L > Bus 13 > Ch 12 > A 12");
  });

  it("stops at a feedback loop through an effect", () => {
    const pb = buildPatchbay(demo({ "/fxrtn/01/mix/13/on": 1, "/fxrtn/01/mix/13/level": [0.5, "-10.0"] }));
    const t = pathTree(pb, "/bus/13", "down")!;
    expect(chains(t).some((c) => c.startsWith("Bus 13 > FX 1L > Bus 13"))).toBe(false);
  });

  it("keeps the paths to the nodes shown", () => {
    const pb = buildPatchbay(demo(), { model: "M32C" });
    const shown = new Set(visibleNodes(pb, { all: false, ports: new Set(["AES50A"]) }).map((n) => n.id));
    expect(chains(pathTree(pb, "/ch/17", "down", (id) => shown.has(id))!)).toEqual(["Ch 17 > Bus 01 > Out 01 > A 1"]);
  });

  it("writes number ranges and lays the columns out", () => {
    expect(ranges([7, 1, 2, 3, 3])).toBe("1-3, 7");
    const pb = buildPatchbay(demo(), { model: "M32C" });
    const nodes = visibleNodes(pb, { all: false });
    const lay = layout(nodes, 30, 24);
    const first = nodes.find((n) => n.col === 0)!;
    expect(lay.y.get(first.id)).toBe(24);
    expect(lay.heads[0]).toMatchObject({ col: 0, y: 0 });
    expect(lay.height).toBeGreaterThan(0);
  });
});
