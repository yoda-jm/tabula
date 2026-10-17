// Console routing (docs: Maillot's protocol document, "/config/routing",
// "/outputs"): where the 32 channel inputs come from, what each output port
// (local XLR, AES50 A and B, card) carries, the sources of the outputs, and
// the headamps. Pure functions: values are read through a Lookup, so
// everything here is unit tested.

const two = (n: number) => String(n).padStart(2, "0");
const three = (n: number) => String(n).padStart(3, "0");

/** Lookup reads mirrored values: the node text of a parameter (enum name, number…) and its raw value. */
export interface Lookup {
  text(addr: string): string | undefined;
  value(addr: string): number | string | undefined;
}

/** Where one channel of a routing block comes from. */
export type PortKind =
  | "local" // local input (headamp) or, in output routing, local XLR input
  | "aes50a"
  | "aes50b"
  | "card"
  | "userin"
  | "userout"
  | "output" // the console's outputs 1-16 (/outputs/main)
  | "p16" // P16 outputs (/outputs/p16)
  | "auxout" // aux outputs 1-6 (/outputs/aux)
  | "monitor" // monitor (control room) L and R
  | "auxin" // local aux inputs
  | "talkback";

export interface PortSource {
  kind: PortKind;
  n: number; // 1-based; monitor: 1 L, 2 R; talkback: 1 internal, 2 external
}

const KIND_LABEL: Record<PortKind, string> = {
  local: "Local",
  aes50a: "AES50 A",
  aes50b: "AES50 B",
  card: "Card",
  userin: "User in",
  userout: "User out",
  output: "Output",
  p16: "P16",
  auxout: "Aux out",
  monitor: "Monitor",
  auxin: "Aux in",
  talkback: "Talkback",
};

export function portLabel(p: PortSource): string {
  if (p.kind === "monitor") return p.n === 1 ? "Monitor L" : "Monitor R";
  if (p.kind === "talkback") return p.n === 1 ? "Talkback int" : "Talkback ext";
  return `${KIND_LABEL[p.kind]} ${p.n}`;
}

const RANGE = /^(AN|A|B|CARD|UIN|UOUT|OUT|P16)(\d+)-(\d+)$/;
const RANGE_KIND: Record<string, PortKind> = {
  AN: "local",
  A: "aes50a",
  B: "aes50b",
  CARD: "card",
  UIN: "userin",
  UOUT: "userout",
  OUT: "output",
  P16: "p16",
};

/**
 * blockSource tells where channel `offset` (0-based) of a routing block
 * comes from, given the block's enum name ("A9-16", "OUT1-8", "P169-16",
 * "AUX1-6/Mon"…). part is the position of a 4-channel output block in its
 * group of 8 (0 for 1-4, 1 for 5-8), for the AUX/CR and AUX/TB blocks.
 */
export function blockSource(name: string, offset: number, part = 0): PortSource | null {
  const m = RANGE.exec(name);
  if (m) {
    const first = Number(m[2]);
    const last = Number(m[3]);
    if (first + offset > last) return null;
    return { kind: RANGE_KIND[m[1]], n: first + offset };
  }
  const k = offset + 4 * part; // position in a group of 8
  switch (name) {
    case "AUX1-6/Mon":
    case "AUX/CR":
      return k < 6 ? { kind: "auxout", n: k + 1 } : { kind: "monitor", n: k - 5 };
    case "AuxIN1-6/TB":
    case "AUX/TB":
      return k < 6 ? { kind: "auxin", n: k + 1 } : { kind: "talkback", n: k - 5 };
  }
  return null;
}

/** blockLabel describes a whole block: "A9-16" → "AES50 A 9-16". */
export function blockLabel(name: string): string {
  const m = RANGE.exec(name);
  if (m) return `${KIND_LABEL[RANGE_KIND[m[1]]]} ${m[2]}-${m[3]}`;
  switch (name) {
    case "AUX1-4": // the input routing's aux block: all six stay local
      return "Local aux in 1-6";
    case "AUX1-6/Mon":
      return "Aux out 1-6, monitor L/R";
    case "AuxIN1-6/TB":
      return "Aux in 1-6, talkback";
    case "AUX/CR":
      return "Aux out / monitor";
    case "AUX/TB":
      return "Aux in / talkback";
  }
  return name;
}

/**
 * auxBlockSource tells where aux input n (1-6) comes from with an
 * /config/routing/IN/AUX value: "AUX1-4" keeps them all local (the console
 * names it so for compatibility, it covers 1-6); "A1-4" takes aux 1-4 from
 * AES50 A 1-4 and leaves the others local.
 */
export function auxBlockSource(name: string, n: number): PortSource {
  const m = /^(AN|A|B|CARD|UIN)1-(\d)$/.exec(name);
  if (m && n <= Number(m[2])) return { kind: RANGE_KIND[m[1]], n };
  return { kind: "auxin", n };
}

// --- channel inputs ---

/** INPUT_BLOCKS are the field names of /config/routing/IN and PLAY. */
export const INPUT_BLOCKS = ["1-8", "9-16", "17-24", "25-32"];

/** inputRouting is the node the inputs come from: IN, or PLAY when routswitch is PLAY. */
export function inputRouting(l: Lookup): "/config/routing/IN" | "/config/routing/PLAY" {
  return Number(l.value("/config/routing/routswitch")) === 1 ? "/config/routing/PLAY" : "/config/routing/IN";
}

/** inputSource tells where console input n (1-32, "In 12" of a channel source) comes from. */
export function inputSource(l: Lookup, n: number): PortSource | null {
  const node = inputRouting(l);
  const block = l.text(`${node}/${INPUT_BLOCKS[Math.floor((n - 1) / 8)]}`)?.trim();
  return block ? blockSource(block, (n - 1) % 8) : null;
}

// --- headamps ---

/** haIndexAddr is the /-ha node telling which headamp feeds a strip (channels and aux ins). */
export function haIndexAddr(strip: string): string | undefined {
  const m = /^\/(ch|auxin)\/(\d\d)$/.exec(strip);
  if (!m) return undefined;
  const i = Number(m[2]) - 1 + (m[1] === "auxin" ? 32 : 0);
  return `/-ha/${two(i)}/index`;
}

export const headampPath = (index: number) => `/headamp/${three(index)}`;

/** headampPort names headamp 0-127: local 1-32, AES50 A 1-48, AES50 B 1-48. */
export function headampPort(index: number): PortSource | null {
  if (index < 0 || index > 127 || !Number.isInteger(index)) return null;
  if (index < 32) return { kind: "local", n: index + 1 };
  if (index < 80) return { kind: "aes50a", n: index - 31 };
  return { kind: "aes50b", n: index - 79 };
}

/** headampOfPort is the inverse of headampPort, for preamp-capable ports. */
export function headampOfPort(p: PortSource): number {
  switch (p.kind) {
    case "local":
      return p.n - 1;
    case "aes50a":
      return 31 + p.n;
    case "aes50b":
      return 79 + p.n;
  }
  return -1;
}

// --- output sources ---

/** OUTPUT_SOURCES counts the values of /outputs/…/src: 0-76. */
export const OUTPUT_SOURCES = 77;

/**
 * outputSource names an output source (/outputs/main|aux|p16|aes|rec/NN/src)
 * and gives the strip it comes from, to show its name.
 */
export function outputSource(n: number): { label: string; strip?: string } {
  if (n <= 0) return { label: "OFF" };
  if (n === 1) return { label: "Main L", strip: "/main/st" };
  if (n === 2) return { label: "Main R", strip: "/main/st" };
  if (n === 3) return { label: "M/C", strip: "/main/m" };
  if (n <= 19) return { label: `MixBus ${two(n - 3)}`, strip: `/bus/${two(n - 3)}` };
  if (n <= 25) return { label: `Matrix ${n - 19}`, strip: `/mtx/${two(n - 19)}` };
  if (n <= 57) return { label: `Ch ${two(n - 25)} direct`, strip: `/ch/${two(n - 25)}` };
  if (n <= 65) return { label: `Aux ${n - 57} direct`, strip: `/auxin/${two(n - 57)}` };
  if (n <= 73) {
    const k = n - 66;
    return { label: `FX ${Math.floor(k / 2) + 1}${k % 2 ? "R" : "L"} direct`, strip: `/fxrtn/${two(k + 1)}` };
  }
  if (n === 74) return { label: "Monitor L" };
  if (n === 75) return { label: "Monitor R" };
  if (n === 76) return { label: "Talkback" };
  return { label: String(n) };
}

/** userSource names a user routing value (/config/userrout/in|out/NN): 0-208. */
export function userSource(n: number): PortSource | null {
  if (n <= 0) return null;
  if (n <= 32) return { kind: "local", n };
  if (n <= 80) return { kind: "aes50a", n: n - 32 };
  if (n <= 128) return { kind: "aes50b", n: n - 80 };
  if (n <= 160) return { kind: "card", n: n - 128 };
  if (n <= 166) return { kind: "auxin", n: n - 160 };
  if (n <= 168) return { kind: "talkback", n: n - 166 };
  if (n <= 184) return { kind: "output", n: n - 168 };
  if (n <= 200) return { kind: "p16", n: n - 184 };
  if (n <= 206) return { kind: "auxout", n: n - 200 };
  if (n <= 208) return { kind: "monitor", n: n - 206 };
  return null;
}

/** stripName is a strip's scribble name, or "" if it has none. */
export function stripName(l: Lookup, strip: string | undefined): string {
  if (!strip) return "";
  const v = l.value(`${strip}/config/name`);
  return typeof v === "string" ? v.trim() : "";
}

/** outputNode is the /outputs node feeding a port source of kind output, p16 or auxout. */
export function outputNode(p: PortSource): string | undefined {
  switch (p.kind) {
    case "output":
      return p.n >= 1 && p.n <= 16 ? `/outputs/main/${two(p.n)}` : undefined;
    case "p16":
      return p.n >= 1 && p.n <= 16 ? `/outputs/p16/${two(p.n)}` : undefined;
    case "auxout":
      return p.n >= 1 && p.n <= 6 ? `/outputs/aux/${two(p.n)}` : undefined;
  }
  return undefined;
}

/**
 * describeSource follows a port source to what it finally carries, as a
 * short text: "Output 1 · MixBus 01 IEM Remy", "AES50 A 12 · Ch 12 Vx Remy".
 * Inputs are described with the channels they feed.
 */
export function describeSource(l: Lookup, p: PortSource | null, depth = 0): string {
  if (!p) return "OFF";
  const base = portLabel(p);
  const node = outputNode(p);
  if (node) {
    const src = Number(l.value(`${node}/src`) ?? 0);
    const o = outputSource(src);
    const name = stripName(l, o.strip);
    return `${base} · ${o.label}${name ? " " + name : ""}`;
  }
  if ((p.kind === "userin" || p.kind === "userout") && depth === 0 && p.n >= 1) {
    const v = Number(l.value(`/config/userrout/${p.kind === "userin" ? "in" : "out"}/${two(p.n)}`) ?? 0);
    return `${base} · ${describeSource(l, userSource(v), 1)}`;
  }
  const ins = channelsOnPort(l, p);
  return ins.length ? `${base} · ${ins.join(", ")}` : base;
}

/** channelSources lists the input source of channels 1-32 (/ch/NN/config/source). */
export function channelSources(l: Lookup): number[] {
  return Array.from({ length: 32 }, (_, i) => Number(l.value(`/ch/${two(i + 1)}/config/source`) ?? 0));
}

/**
 * stripsUsingSource names the channels and aux inputs whose source is
 * source (/ch/NN/config/source numbering: 1-32 inputs, 33-38 aux…):
 * ["Ch 12 Vx Remy", "Ch 17 Remy"].
 */
export function stripsUsingSource(l: Lookup, source: number): string[] {
  const out: string[] = [];
  channelSources(l).forEach((src, i) => {
    if (src === source) {
      const name = stripName(l, `/ch/${two(i + 1)}`);
      out.push(`Ch ${two(i + 1)}${name ? " " + name : ""}`);
    }
  });
  for (let n = 1; n <= 8; n++) {
    if (Number(l.value(`/auxin/${two(n)}/config/source`)) === source) {
      const name = stripName(l, `/auxin/${two(n)}`);
      out.push(`Aux ${n}${name ? " " + name : ""}`);
    }
  }
  return out;
}

/**
 * channelsOnPort names the channels fed by an input port (through the
 * input routing and their source): ["Ch 12 Vx Remy", "Ch 17 Remy"].
 */
export function channelsOnPort(l: Lookup, p: PortSource): string[] {
  const out: string[] = [];
  for (let n = 1; n <= 32; n++) {
    const s = inputSource(l, n);
    if (s && s.kind === p.kind && s.n === p.n) out.push(...stripsUsingSource(l, n));
  }
  return out;
}

// --- output ports ---

export interface Port {
  id: "OUT" | "AES50A" | "AES50B" | "CARD";
  label: string;
  /** routing node and its block fields */
  node: string;
  blocks: string[];
  /** channels per block */
  size: number;
  channels: number;
}

const AES50_BLOCKS = ["1-8", "9-16", "17-24", "25-32", "33-40", "41-48"];

export const PORTS: Port[] = [
  { id: "AES50A", label: "AES50 A", node: "/config/routing/AES50A", blocks: AES50_BLOCKS, size: 8, channels: 48 },
  { id: "AES50B", label: "AES50 B", node: "/config/routing/AES50B", blocks: AES50_BLOCKS, size: 8, channels: 48 },
  { id: "CARD", label: "Card", node: "/config/routing/CARD", blocks: INPUT_BLOCKS, size: 8, channels: 32 },
  { id: "OUT", label: "Local outputs", node: "/config/routing/OUT", blocks: ["1-4", "5-8", "9-12", "13-16"], size: 4, channels: 16 },
];

export const portById = (id: Port["id"]) => PORTS.find((p) => p.id === id)!;

/** portChannelSource tells what channel n (1-based) of an output port carries. */
export function portChannelSource(l: Lookup, port: Port, n: number): PortSource | null {
  const b = Math.floor((n - 1) / port.size);
  const name = l.text(`${port.node}/${port.blocks[b]}`)?.trim();
  if (!name) return null;
  // local output blocks of 4 alternate between the "1-4" and "5-8" lists
  return blockSource(name, (n - 1) % port.size, port.size === 4 ? b % 2 : 0);
}

/**
 * outputDestinations lists, for each console output 1-16, the port
 * channels that carry it: ["AES50 A 1", "Card 9"].
 */
export function outputDestinations(l: Lookup): string[][] {
  const out: string[][] = Array.from({ length: 16 }, () => []);
  for (const port of PORTS) {
    for (let n = 1; n <= port.channels; n++) {
      const s = portChannelSource(l, port, n);
      if (s?.kind === "output" && s.n >= 1 && s.n <= 16) out[s.n - 1].push(`${port.id === "OUT" ? "XLR out" : port.label} ${n}`);
    }
  }
  return out;
}
