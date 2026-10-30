// A small PDF writer, for the patch list (lib/patchlist.ts): pages of text,
// lines and filled boxes, in the Helvetica every PDF reader has, so no
// font is embedded and nothing is installed. Text is written in
// WinAnsiEncoding (Latin-1 and a few more): French and most European names
// print as typed, other characters as "?". Positions are in points (1/72
// inch) from the top left of the page; text is placed by its baseline.

export type FontName = "regular" | "bold";

/** Rgb is a colour, each part from 0 to 1. */
export type Rgb = readonly [number, number, number];

export interface PaperSize {
  w: number;
  h: number;
}

export const A4: PaperSize = { w: 595.28, h: 841.89 };
export const LETTER: PaperSize = { w: 612, h: 792 };

export const BLACK: Rgb = [0, 0, 0];

// Advance widths of Helvetica and Helvetica-Bold (Adobe's AFM files), in
// thousandths of the font size, for the WinAnsi codes 32-255.
const WIDTHS: Record<FontName, number[]> = {
  regular: [
    278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
    556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833,
    722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556,
    556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334,
    260, 334, 584, 0, 556, 0, 222, 556, 333, 1000, 556, 556, 333, 1000, 667, 333, 1000, 0, 611, 0, 0, 222, 222, 333,
    333, 350, 556, 1000, 333, 1000, 500, 333, 944, 0, 500, 667, 278, 333, 556, 556, 556, 556, 260, 556, 333, 737, 370,
    556, 584, 333, 737, 333, 400, 584, 333, 333, 333, 556, 537, 278, 333, 333, 365, 556, 834, 834, 834, 611, 667, 667,
    667, 667, 667, 667, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, 722, 722, 778, 778, 778, 778, 778, 584, 778,
    722, 722, 722, 722, 667, 667, 611, 556, 556, 556, 556, 556, 556, 889, 500, 556, 556, 556, 556, 278, 278, 278, 278,
    556, 556, 556, 556, 556, 556, 556, 584, 611, 556, 556, 556, 556, 500, 556, 500,
  ],
  bold: [
    278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
    556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833,
    722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611,
    556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389,
    280, 389, 584, 0, 556, 0, 278, 556, 500, 1000, 556, 556, 333, 1000, 667, 333, 1000, 0, 611, 0, 0, 278, 278, 500,
    500, 350, 556, 1000, 333, 1000, 556, 333, 944, 0, 500, 667, 278, 333, 556, 556, 556, 556, 280, 556, 333, 737, 370,
    556, 584, 333, 737, 333, 400, 584, 333, 333, 333, 611, 556, 278, 333, 333, 365, 556, 834, 834, 834, 611, 722, 722,
    722, 722, 722, 722, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, 722, 722, 778, 778, 778, 778, 778, 584, 778,
    722, 722, 722, 722, 667, 667, 611, 556, 556, 556, 556, 556, 556, 889, 556, 556, 556, 556, 556, 278, 278, 278, 278,
    611, 611, 611, 611, 611, 611, 611, 584, 611, 611, 611, 611, 611, 556, 611, 556,
  ],
};

/** The cap height of Helvetica, in thousandths of the font size. */
const CAP_HEIGHT = 718;

// WinAnsi codes 128-159 that are not Latin-1, by Unicode code point.
const WIN_ANSI: Record<number, number> = {
  0x20ac: 0x80, 0x201a: 0x82, 0x0192: 0x83, 0x201e: 0x84, 0x2026: 0x85, 0x2020: 0x86, 0x2021: 0x87, 0x02c6: 0x88,
  0x2030: 0x89, 0x0160: 0x8a, 0x2039: 0x8b, 0x0152: 0x8c, 0x017d: 0x8e, 0x2018: 0x91, 0x2019: 0x92, 0x201c: 0x93,
  0x201d: 0x94, 0x2022: 0x95, 0x2013: 0x96, 0x2014: 0x97, 0x02dc: 0x98, 0x2122: 0x99, 0x0161: 0x9a, 0x203a: 0x9b,
  0x0153: 0x9c, 0x017e: 0x9e, 0x0178: 0x9f,
};

/** winAnsi is the WinAnsiEncoding code of a character, 63 ("?") for one it does not have. */
export function winAnsi(ch: string): number {
  const c = ch.codePointAt(0) ?? 63;
  if ((c >= 0x20 && c <= 0x7e) || (c >= 0xa0 && c <= 0xff)) return c;
  return WIN_ANSI[c] ?? 63;
}

/** textWidth measures s, printed at size points. */
export function textWidth(s: string, size: number, font: FontName = "regular"): number {
  let w = 0;
  for (const ch of s) w += WIDTHS[font][winAnsi(ch) - 32] || 556;
  return (w * size) / 1000;
}

/** fit shortens s with "…" so it takes width at most. */
export function fit(s: string, width: number, size: number, font: FontName = "regular"): string {
  if (textWidth(s, size, font) <= width) return s;
  const chars = [...s];
  while (chars.length && textWidth(chars.join("").trimEnd() + "…", size, font) > width) chars.pop();
  return chars.length ? chars.join("").trimEnd() + "…" : "";
}

/**
 * wrap breaks s into lines of width at most, between words (a word longer
 * than a line is cut), and keeps lines lines at most: the last one is then
 * shortened with "…".
 */
export function wrap(s: string, width: number, size: number, font: FontName = "regular", lines = Infinity): string[] {
  const out: string[] = [];
  let cur = "";
  for (let w of s.split(/\s+/).filter(Boolean)) {
    const next = cur ? `${cur} ${w}` : w;
    if (textWidth(next, size, font) <= width) {
      cur = next;
      continue;
    }
    if (cur) out.push(cur);
    while (textWidth(w, size, font) > width) {
      const chars = [...w];
      let n = Math.max(1, chars.length - 1);
      while (n > 1 && textWidth(chars.slice(0, n).join(""), size, font) > width) n--;
      out.push(chars.slice(0, n).join(""));
      w = chars.slice(n).join("");
    }
    cur = w;
  }
  if (cur) out.push(cur);
  if (out.length <= lines) return out;
  return [...out.slice(0, lines - 1), fit(out.slice(lines - 1).join(" "), width, size, font)];
}

/** num writes a number for the content stream: at most 2 decimals, no exponent. */
const num = (n: number) => String(Math.round(n * 100) / 100);

const rgb = (c: Rgb) => c.map(num).join(" ");

/** str writes a literal string in WinAnsiEncoding, one character per byte. */
function str(s: string): string {
  let out = "(";
  for (const ch of s) {
    const c = winAnsi(ch);
    if (c === 0x28 || c === 0x29 || c === 0x5c) out += "\\";
    out += String.fromCharCode(c);
  }
  return out + ")";
}

/** textString writes a text string of the document's information, in UTF-16. */
function textString(s: string): string {
  let hex = "<FEFF";
  for (let i = 0; i < s.length; i++) hex += s.charCodeAt(i).toString(16).toUpperCase().padStart(4, "0");
  return hex + ">";
}

export interface TextOptions {
  font?: FontName;
  color?: Rgb;
  /** where x is: the start of the text (the default), its end or its middle */
  align?: "left" | "right" | "center";
}

/**
 * A page: boxes are drawn first, then the watermark, then the text and
 * lines, so the watermark shows through the shaded rows and never covers
 * a word.
 */
export class PdfPage {
  private readonly back: string[] = [];
  private readonly mark: string[] = [];
  private readonly front: string[] = [];

  constructor(
    readonly w: number,
    readonly h: number,
  ) {}

  /** box fills a rectangle, x and y its top left corner. */
  box(x: number, y: number, w: number, h: number, fill: Rgb, stroke?: Rgb) {
    const ops = `${rgb(fill)} rg ${num(x)} ${num(this.h - y - h)} ${num(w)} ${num(h)} re`;
    this.back.push(stroke ? `${rgb(stroke)} RG 0.5 w ${ops} B` : `${ops} f`);
  }

  line(x1: number, y1: number, x2: number, y2: number, color: Rgb = BLACK, width = 0.5) {
    this.front.push(`${rgb(color)} RG ${num(width)} w ${num(x1)} ${num(this.h - y1)} m ${num(x2)} ${num(this.h - y2)} l S`);
  }

  /** text writes s at size points, its baseline at y. */
  text(x: number, y: number, s: string, size: number, opts: TextOptions = {}) {
    if (!s) return;
    const font = opts.font ?? "regular";
    const w = textWidth(s, size, font);
    const at = opts.align === "right" ? x - w : opts.align === "center" ? x - w / 2 : x;
    this.front.push(
      `BT /${font === "bold" ? "F2" : "F1"} ${num(size)} Tf ${rgb(opts.color ?? BLACK)} rg ` +
        `1 0 0 1 ${num(at)} ${num(this.h - y)} Tm ${str(s)} Tj ET`,
    );
  }

  /** watermark writes s across the page, from its bottom left to its top right, behind the text. */
  watermark(s: string, color: Rgb, font: FontName = "bold") {
    const angle = Math.atan2(this.h, this.w);
    const c = Math.cos(angle);
    const sin = Math.sin(angle);
    // the text spans 60% of the diagonal
    const size = (0.6 * Math.hypot(this.w, this.h)) / textWidth(s, 1, font);
    const w = textWidth(s, size, font);
    const capH = (CAP_HEIGHT * size) / 1000;
    // its middle at the middle of the page: back half its width along the
    // text, down half its height across it
    const x = this.w / 2 - (w / 2) * c + (capH / 2) * sin;
    const y = this.h / 2 - (w / 2) * sin - (capH / 2) * c;
    // marked as a watermark: readers that copy or read the text aloud leave it out
    this.mark.push(
      `/Artifact << /Type /Pagination /Subtype /Watermark >> BDC BT /${font === "bold" ? "F2" : "F1"} ${num(size)} Tf ${rgb(color)} rg ` +
        `${c.toFixed(4)} ${sin.toFixed(4)} ${(-sin).toFixed(4)} ${c.toFixed(4)} ${num(x)} ${num(y)} Tm ${str(s)} Tj ET EMC`,
    );
  }

  content(): string {
    return [...this.back, ...this.mark, ...this.front].join("\n");
  }
}

export interface DocInfo {
  title?: string;
  author?: string;
  creator?: string;
}

/** pdfDate writes a date of the document's information: D:20261030214500+01'00'. */
export function pdfDate(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  const off = -d.getTimezoneOffset();
  const zone = off === 0 ? "Z" : `${off > 0 ? "+" : "-"}${p(Math.floor(Math.abs(off) / 60))}'${p(Math.abs(off) % 60)}'`;
  return `D:${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}${zone}`;
}

/** A PDF document: pages added one after the other, then written by bytes(). */
export class PdfDoc {
  readonly pages: PdfPage[] = [];

  constructor(
    readonly paper: PaperSize = A4,
    readonly info: DocInfo = {},
  ) {}

  addPage(): PdfPage {
    const p = new PdfPage(this.paper.w, this.paper.h);
    this.pages.push(p);
    return p;
  }

  /** bytes writes the document, made at date. */
  bytes(date = new Date()): Uint8Array {
    // objects: 1 catalog, 2 page tree, 3 and 4 the fonts, 5 the information,
    // then each page and its content
    const objs: string[] = [];
    const pageObj = (i: number) => 6 + 2 * i;
    objs.push("<< /Type /Catalog /Pages 2 0 R >>");
    objs.push(`<< /Type /Pages /Kids [${this.pages.map((_, i) => `${pageObj(i)} 0 R`).join(" ")}] /Count ${this.pages.length} >>`);
    objs.push("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>");
    objs.push("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>");
    const info = [
      this.info.title && `/Title ${textString(this.info.title)}`,
      this.info.author && `/Author ${textString(this.info.author)}`,
      this.info.creator && `/Creator ${textString(this.info.creator)}`,
      `/Producer ${textString("Tabula")}`,
      `/CreationDate (${pdfDate(date)})`,
    ].filter(Boolean);
    objs.push(`<< ${info.join(" ")} >>`);
    this.pages.forEach((p, i) => {
      const content = p.content();
      objs.push(
        `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${num(p.w)} ${num(p.h)}] ` +
          `/Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents ${pageObj(i) + 1} 0 R >>`,
      );
      objs.push(`<< /Length ${content.length} >>\nstream\n${content}\nendstream`);
    });

    // every character of out is one byte: its length is the offset
    let out = "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n";
    const offsets: number[] = [];
    objs.forEach((o, i) => {
      offsets.push(out.length);
      out += `${i + 1} 0 obj\n${o}\nendobj\n`;
    });
    const xref = out.length;
    out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
    for (const off of offsets) out += `${String(off).padStart(10, "0")} 00000 n \n`;
    out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R /Info 5 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
    const bytes = new Uint8Array(out.length);
    for (let i = 0; i < out.length; i++) bytes[i] = out.charCodeAt(i);
    return bytes;
  }
}
