import { describe, expect, it } from "vitest";
import type { InputRow, PatchSettings } from "./patchlist";
import {
  NEW_SETTINGS,
  boxesLabel,
  boxesOf,
  consoleLabel,
  fileName,
  inputRows,
  listed,
  noteOf,
  outputRows,
  patchlistPdf,
  reportedBoxes,
} from "./patchlist";
import type { Lookup } from "./routing";

/** A Lookup over a band's show: a DL32 on AES50 A, a backing track from the card. */
function show(extra: Record<string, number | string> = {}): Lookup {
  const v: Record<string, number | string> = {
    "/config/routing/routswitch": 0,
    "/config/routing/IN/1-8": "A1-8",
    "/config/routing/IN/9-16": "A9-16",
    "/config/routing/IN/17-24": "A17-24",
    "/config/routing/IN/25-32": "CARD25-32",
    "/config/routing/IN/AUX": "AUX1-4",
    "/config/routing/AES50A/1-8": "OUT1-8",
    "/config/routing/AES50A/9-16": "OUT9-16",
    "/ch/01/config/source": 1,
    "/ch/01/config/name": "Hang",
    "/ch/01/config/color": 1,
    "/ch/06/config/source": 6,
    "/ch/06/config/name": "Basse",
    "/headamp/037/phantom": 1, // AES50 A 6
    "/config/chlink/7-8": 1,
    "/ch/07/config/source": 7,
    "/ch/07/config/name": "Keys L",
    "/ch/08/config/source": 8,
    "/ch/08/config/name": "Keys R",
    "/ch/11/config/source": 11,
    "/ch/11/config/name": "-", // a spacer
    "/ch/12/config/source": 12,
    "/ch/12/config/name": "Vx Theo",
    "/ch/17/config/source": 12, // Theo's own voice, for his in-ears
    "/ch/17/config/name": "Theo IEM",
    "/ch/25/config/source": 25,
    "/ch/25/config/name": "Track",
    "/ch/30/config/name": "Spare",
    "/auxin/01/config/source": 33,
    "/auxin/01/config/name": "Phone",
    "/auxin/07/config/source": 39,
    "/auxin/07/config/name": "USB",
    "/outputs/main/01/src": 4, // MixBus 01
    "/bus/01/config/name": "IEM Theo",
    "/outputs/main/02/src": 5,
    "/bus/02/config/name": "-",
    "/outputs/main/13/src": 22, // Matrix 3
    "/mtx/03/config/name": "RappelL",
    "/outputs/main/13/delay/on": 1,
    "/outputs/main/13/delay/time": "29.2",
    ...extra,
  };
  return {
    text: (a) => (v[a] === undefined ? undefined : String(v[a])),
    value: (a) => v[a],
  };
}

const settings = (s: Partial<PatchSettings> = {}): PatchSettings => ({ ...NEW_SETTINGS, ...s });
const row = (rows: InputRow[], key: string) => rows.find((r) => r.key === key)!;

describe("patch list", () => {
  it("names the boxes the console reports", () => {
    expect(reportedBoxes("Y")).toBe("DL32");
    expect(reportedBoxes("Y000000")).toBe("DL32");
    expect(reportedBoxes('"NN"')).toBe("DL16 + DL16");
    expect(reportedBoxes("DL32")).toBe("DL32");
    expect(reportedBoxes("")).toBe("");
    expect(reportedBoxes(undefined)).toBe("");
    expect(reportedBoxes("not a box")).toBe("");
    expect(boxesOf(show({ "/-stat/aes50/A": "Y" }), settings())).toEqual({ A: "DL32", B: "" });
    expect(boxesOf(show({ "/-stat/aes50/A": "Y" }), settings({ box_a: " S32 " }))).toEqual({ A: "S32", B: "" });
    expect(boxesLabel({ A: "DL32", B: "DL16" })).toBe("DL32 on AES50 A, DL16 on AES50 B");
    expect(consoleLabel("M32C")).toBe("Midas M32C");
    expect(consoleLabel("X32RACK")).toBe("Behringer X32RACK");
    expect(consoleLabel(undefined)).toBe("");
  });

  it("lists the channels with their sockets", () => {
    const rows = inputRows(show(), { A: "DL32", B: "" });
    expect(rows).toHaveLength(40);
    expect(row(rows, "/ch/01")).toMatchObject({ ch: "1", name: "Hang", color: 1, socket: "DL32 in 1", onStage: true, phantom: false, auto: true });
    expect(row(rows, "/ch/06")).toMatchObject({ socket: "DL32 in 6", phantom: true, auto: true });
    expect(row(rows, "/ch/07").hints).toEqual(["stereo L (ch 7-8)"]);
    expect(row(rows, "/ch/08").hints).toEqual(["stereo R (ch 7-8)"]);
    // a placeholder name is no name: left out
    expect(row(rows, "/ch/11")).toMatchObject({ name: "", socket: "DL32 in 11", auto: false });
    // a second channel on a socket is left out, and says so
    expect(row(rows, "/ch/12").auto).toBe(true);
    expect(row(rows, "/ch/17")).toMatchObject({ socket: "DL32 in 12", hints: ["same input as ch 12"], auto: false });
    // nothing plugged on stage: the card, the USB player, no source
    expect(row(rows, "/ch/25")).toMatchObject({ socket: "Card 25", onStage: false, auto: false });
    expect(row(rows, "/ch/30")).toMatchObject({ socket: "OFF", from: "", auto: false });
    expect(row(rows, "/auxin/01")).toMatchObject({ ch: "Aux 1", socket: "Aux in 1", onStage: true, auto: true });
    expect(row(rows, "/auxin/07")).toMatchObject({ socket: "USB player L", onStage: false, auto: false });
    // no box named: the port
    expect(row(inputRows(show(), { A: "", B: "" }), "/ch/01").socket).toBe("AES50 A 1");
  });

  it("follows a user input to its socket", () => {
    const l = show({ "/config/routing/IN/1-8": "UIN1-8", "/config/userrout/in/01": 40 });
    expect(row(inputRows(l, { A: "DL32", B: "" }), "/ch/01").socket).toBe("DL32 in 8");
  });

  it("lists the outputs with what they carry", () => {
    const l = show();
    const ins = inputRows(l, { A: "DL32", B: "" });
    const outs = outputRows(l, { A: "DL32", B: "" }, "M32C", ins);
    expect(outs).toHaveLength(32); // the M32C has no sockets of its own
    expect(outs[0]).toMatchObject({ key: "AES50A:1", group: "DL32 on AES50 A", socket: "DL32 out 1", carries: "MixBus 01 · IEM Theo", auto: true });
    expect(outs[1]).toMatchObject({ carries: "MixBus 02", auto: true });
    expect(outs[2]).toMatchObject({ carries: "OFF", auto: false });
    expect(outs[12]).toMatchObject({ socket: "DL32 out 13", carries: "Matrix 3 · RappelL", hints: ["delay 29.2 ms"] });
    // nothing on AES50 B: left out
    expect(outs[16]).toMatchObject({ key: "AES50B:1", group: "AES50 B", socket: "AES50 B out 1", auto: false });

    // a console with sockets: XLR outputs ticked, the others listed unticked
    const m32 = outputRows(l, { A: "DL32", B: "" }, "M32", ins);
    expect(m32).toHaveLength(32 + 16 + 6 + 16 + 2);
    expect(m32.find((r) => r.key === "/outputs/aux/01")).toMatchObject({ socket: "Aux out 1", auto: false });
    expect(m32.find((r) => r.key === "/outputs/aes/02")).toMatchObject({ socket: "AES R" });

    // a DL16 has 8 outputs
    expect(outputRows(l, { A: "DL16", B: "" }, "M32C", ins).filter((r) => r.key.startsWith("AES50A:"))).toHaveLength(8);
    // no box named, but the channels come from it
    expect(outputRows(l, { A: "", B: "" }, "M32C", inputRows(l, { A: "", B: "" }))[0]).toMatchObject({ socket: "AES50 A out 1", auto: true });
  });

  it("prints what is ticked, with the notes", () => {
    const s = settings({ rows: { "/ch/17": true, "/ch/01": false }, notes: { "/ch/07": "DI, stereo" } });
    const rows = inputRows(show(), { A: "DL32", B: "" });
    expect(listed(s, row(rows, "/ch/17"))).toBe(true);
    expect(listed(s, row(rows, "/ch/01"))).toBe(false);
    expect(listed(s, row(rows, "/ch/06"))).toBe(true);
    expect(noteOf(s, row(rows, "/ch/07"))).toBe("DI, stereo · stereo L (ch 7-8)");
    expect(noteOf(s, row(rows, "/ch/06"))).toBe("");
  });

  it("names the file", () => {
    expect(fileName("Les Zéniths !", new Date(2026, 9, 30))).toBe("patch-list-les-zeniths-2026-10-30.pdf");
    expect(fileName("", new Date(2026, 0, 2))).toBe("patch-list-2026-01-02.pdf");
  });

  it("draws the PDF", () => {
    const l = show();
    const boxes = { A: "DL32", B: "" };
    const ins = inputRows(l, boxes);
    const outs = outputRows(l, boxes, "M32C", ins);
    const meta = { model: "M32C", version: "v1.2.3", date: "30 October 2026, 21:45" };
    const read = (b: Uint8Array) => Array.from(b, (c) => String.fromCharCode(c)).join("");
    const pdf = read(patchlistPdf(settings({ title: "The Band", notes: { "/ch/01": "Mic à pince" } }), ins, outs, boxes, meta));
    expect(pdf.startsWith("%PDF-1.4")).toBe(true);
    for (const t of ["(The Band)", "(PATCH LIST)", "(Hang)", "(DL32 in 1)", "(48V)", "(Mic \xe0 pince)", "(Midas M32C)", "(DL32 on AES50 A)", "(Matrix 3 \xb7 RappelL)", "(delay 29.2 ms)"]) {
      expect(pdf).toContain(t + " Tj");
    }
    expect(pdf).not.toContain("(Theo IEM)"); // left out
    expect(pdf).toContain("(Generated by Tabula v1.2.3 \xb7 30 October 2026, 21:45) Tj"); // the foot
    expect(pdf).toContain("(Generated by Tabula) Tj"); // the watermark
    expect(pdf).toContain("(Page 1 of 1) Tj");

    // every row ticked: more pages, their rows under the column titles again
    const rows = Object.fromEntries([...ins, ...outs].map((r) => [r.key, true]));
    const long = read(patchlistPdf(settings({ rows, watermark: false }), ins, outs, boxes, meta));
    const pages = Number(/\/Count (\d+)/.exec(long)![1]);
    expect(pages).toBeGreaterThan(1);
    expect(long).toContain(`(Page ${pages} of ${pages}) Tj`);
    expect(long).toMatch(/\((Inputs|Outputs) \\\(continued\\\)\) Tj/); // parentheses escaped
    expect(long).not.toContain("(Generated by Tabula) Tj"); // no watermark, the foot stays
    expect(long).toContain("(Patch list) Tj"); // no title
  });
});
