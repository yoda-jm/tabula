// The patchbay: every path a signal takes through the console, from the
// socket it comes in by (a stage box preamp on AES50, the card, a local
// input) to the sockets it goes out by (the stage box outputs, the card
// tracks a computer records, local outputs), through the channels, the
// buses, the main mix, the matrices and the outputs 1-16. A graph of nodes
// in seven columns, in the order a signal goes through them, and the links
// between them, built from the mirrored values (a Lookup) by pure
// functions, so everything here is unit tested.

import { range, two } from "./num";
import type { Lookup, PortSource } from "./routing";
import {
  PORTS,
  auxBlockSource,
  headampOfPort,
  headampPath,
  inputRouting,
  inputSource,
  outputSource,
  portChannelSource,
  stripName,
  userSource,
} from "./routing";

export const COLUMNS = ["Inputs", "Channels", "Buses", "Main", "Matrices", "Outputs", "Goes out on"];
export type Column = 0 | 1 | 2 | 3 | 4 | 5 | 6;

/** COL numbers the columns: a link always goes to a column on the right, but for an effect's return. */
export const COL = { in: 0, strip: 1, bus: 2, main: 3, mtx: 4, out: 5, port: 6 } as const;

/** isMix tells the buses, the main mix and the matrices. */
const isMix = (n: PbNode) => n.col >= COL.bus && n.col <= COL.mtx && n.id !== "mon";

export interface PbNode {
  id: string;
  col: Column;
  /** the heading it is listed under: "AES50 A", "Channels", "Card tracks"… */
  group: string;
  /** sorts the nodes of a column: the group first, then the number */
  order: number;
  label: string; // "A 12", "Ch 12", "Out 03", "Track 17"
  /** the strip's name; for an output or a port channel, the name of what it carries */
  name: string;
  /** a short fact: the headamp gain, an output's delay, what a port channel carries ("Out 01 · Theo IEM") */
  detail?: string;
  /** the physical socket, when the label does not say it: "box in 12", "box out 3" */
  socket?: string;
  /** the output port of a port channel: AES50A, AES50B, CARD or OUT */
  port?: string;
  phantom?: boolean;
  muted?: boolean;
  /** the strip's colour (/config/color), for channels and mixes */
  color?: number;
  /** the strip's path, for its meter */
  strip?: string;
  /** a card track recorded with the sessions */
  recorded?: boolean;
  /** makes its own signal: sockets, the monitor, a channel fed by an effect or a bus */
  origin?: boolean;
  /** a socket the console does not have (a local input of an M32C) */
  missing?: boolean;
  /** a mix nothing is sent to */
  idle?: boolean;
}

export type LinkKind = "patch" | "send" | "fx";

export interface PbLink {
  from: string;
  to: string;
  /** patch: routing; send: a send or an assign to a mix; fx: a mix feeding an effect, whose returns are channels */
  kind: LinkKind;
  /** "In 12", "PRE 0.0", "LR", "POST · 29.2 ms" */
  label: string;
  /** from a muted strip */
  muted?: boolean;
}

export interface Warning {
  node: string;
  text: string;
}

export interface Patchbay {
  nodes: Map<string, PbNode>;
  /** the links on a path: from a socket or a mix, through the console */
  links: PbLink[];
  warnings: Warning[];
}

export interface PatchbayOptions {
  /** the console model ("M32C"): which local sockets it has */
  model?: string;
  /** the card tracks recorded with the sessions (tabula's audio settings) */
  recorded?: number[];
}

export interface LocalIO {
  ins: number; // inputs with a preamp
  outs: number; // XLR outputs
  aux: number; // aux in sockets
}

/** LOCAL_IO counts each model's own sockets: an M32C has none, all its inputs come over AES50 or the card. */
const LOCAL_IO: Record<string, LocalIO> = {
  X32: { ins: 32, outs: 16, aux: 6 },
  M32: { ins: 32, outs: 16, aux: 6 },
  X32RACK: { ins: 16, outs: 8, aux: 6 },
  X32C: { ins: 16, outs: 8, aux: 6 },
  X32P: { ins: 16, outs: 8, aux: 6 },
  M32R: { ins: 16, outs: 8, aux: 6 },
  X32CORE: { ins: 0, outs: 0, aux: 6 },
  M32C: { ins: 0, outs: 0, aux: 0 },
};

export const localIO = (model?: string): LocalIO => LOCAL_IO[(model ?? "").toUpperCase()] ?? { ins: 32, outs: 16, aux: 6 };

// Node ids: sockets "in:KIND:N", strips and mixes their path ("/ch/12",
// "/main/st"), outputs their node ("/outputs/main/03"), the monitor "mon",
// output port channels "PORT:N" ("AES50A:3", "CARD:17", "OUT:5").

const STRIPS = [
  ...range(1, 32).map((n) => ({ path: `/ch/${two(n)}`, label: `Ch ${two(n)}`, group: "Channels", order: n })),
  ...range(1, 8).map((n) => ({ path: `/auxin/${two(n)}`, label: `Aux ${n}`, group: "Aux inputs", order: 100 + n })),
  ...range(1, 8).map((n) => ({
    path: `/fxrtn/${two(n)}`,
    label: `FX ${Math.ceil(n / 2)}${n % 2 ? "L" : "R"}`,
    group: "FX returns",
    order: 200 + n,
  })),
];

const MIXES: { path: string; label: string; group: string; order: number; col: Column }[] = [
  ...range(1, 16).map((n) => ({ path: `/bus/${two(n)}`, label: `Bus ${two(n)}`, group: "Buses", order: n, col: COL.bus })),
  { path: "/main/st", label: "LR", group: "Main", order: 100, col: COL.main },
  { path: "/main/m", label: "M/C", group: "Main", order: 101, col: COL.main },
  ...range(1, 6).map((n) => ({ path: `/mtx/${two(n)}`, label: `Mtx ${n}`, group: "Matrices", order: 200 + n, col: COL.mtx })),
];

const OUTPUTS = [
  { node: "/outputs/main", count: 16, label: (n: number) => `Out ${two(n)}`, group: "Outputs 1-16", order: 0 },
  { node: "/outputs/aux", count: 6, label: (n: number) => `Aux out ${n}`, group: "Aux outputs", order: 100 },
  { node: "/outputs/p16", count: 16, label: (n: number) => `P16 ${n}`, group: "P16 (Ultranet)", order: 200 },
  { node: "/outputs/aes", count: 2, label: (n: number) => `AES ${n === 1 ? "L" : "R"}`, group: "AES/EBU", order: 300 },
  { node: "/outputs/rec", count: 2, label: (n: number) => `Rec ${n === 1 ? "L" : "R"}`, group: "2-track (USB recorder)", order: 400 },
];

const SOCKET_ORDER: Record<string, number> = { local: 0, aes50a: 1, aes50b: 2, card: 3, auxin: 4, usb: 5, talkback: 6 };

const on = (l: Lookup, a: string) => Number(l.value(a) ?? 0) === 1;
/** up tells whether a level is above −∞ (its raw value is 0 at −∞). */
const up = (l: Lookup, a: string) => Number(l.value(a) ?? 0) > 0;
const txt = (l: Lookup, a: string) => {
  const t = (l.text(a) ?? "").trim();
  return t === "-oo" ? "−∞" : t.replace(/^"(.*)"$/, "$1");
};

class Builder {
  nodes = new Map<string, PbNode>();
  links: PbLink[] = [];
  warnings: Warning[] = [];
  missing: number[] = [];
  readonly io: LocalIO;

  constructor(
    readonly l: Lookup,
    readonly opts: PatchbayOptions,
  ) {
    this.io = localIO(opts.model);
  }

  add(n: PbNode): string {
    if (!this.nodes.has(n.id)) this.nodes.set(n.id, n);
    return n.id;
  }

  link(from: string | null, to: string, kind: LinkKind, label: string) {
    if (from) this.links.push({ from, to, kind, label });
  }

  /** device names the stage box on an AES50 port, when the console tells it. */
  device(port: "A" | "B"): string {
    return txt(this.l, `/-stat/aes50/${port}`);
  }

  socket(kind: string, n: number): string {
    const id = `in:${kind}:${n}`;
    if (this.nodes.has(id)) return id;
    const order = SOCKET_ORDER[kind] * 1000 + n;
    const node: PbNode = { id, col: COL.in, group: "", order, label: "", name: "", origin: true };
    switch (kind) {
      case "local":
        Object.assign(node, { group: "Local inputs", label: `Local ${n}`, socket: `local input ${n}` });
        if (n > this.io.ins) {
          node.missing = true;
          this.missing.push(n);
        }
        break;
      case "aes50a":
      case "aes50b": {
        const port = kind === "aes50a" ? "A" : "B";
        const dev = this.device(port);
        Object.assign(node, { group: `AES50 ${port}${dev ? " · " + dev : ""}`, label: `${port} ${n}`, socket: `box in ${n}` });
        break;
      }
      case "card":
        Object.assign(node, { group: "Card playback", label: `Card ${n}`, socket: `card input ${n}` });
        break;
      case "auxin":
        Object.assign(node, { group: "Aux in sockets", label: `Aux in ${n}`, socket: `aux in ${n}` });
        break;
      case "usb":
        Object.assign(node, { group: "USB stick player", label: n === 1 ? "USB L" : "USB R", socket: "the USB stick's player" });
        break;
      case "talkback":
        Object.assign(node, { group: "Talkback", label: "Talkback", name: n === 1 ? "internal" : "external" });
        break;
    }
    const ha = kind === "local" || kind === "aes50a" || kind === "aes50b" ? headampOfPort({ kind, n } as PortSource) : -1;
    if (ha >= 0) {
      const gain = txt(this.l, `${headampPath(ha)}/gain`);
      if (gain) node.detail = `${gain} dB`;
      node.phantom = on(this.l, `${headampPath(ha)}/phantom`);
    }
    return this.add(node);
  }

  monitor(): string {
    return this.add({ id: "mon", col: COL.mtx, group: "Monitor", order: 300, label: "Monitor", name: "L/R", origin: true });
  }

  /**
   * port gives the node a routing block's channel comes from (a socket, an
   * output, the monitor), following the user routing one step; via names
   * the user patch it went through.
   */
  port(p: PortSource | null, depth = 0): { id: string | null; via: string } {
    if (!p) return { id: null, via: "" };
    switch (p.kind) {
      case "local":
      case "aes50a":
      case "aes50b":
      case "card":
      case "talkback":
        return { id: this.socket(p.kind, p.n), via: "" };
      case "auxin":
        return { id: p.n <= this.io.aux ? this.socket(p.kind, p.n) : null, via: "" };
      case "output":
        return { id: p.n >= 1 && p.n <= 16 ? `/outputs/main/${two(p.n)}` : null, via: "" };
      case "p16":
        return { id: p.n >= 1 && p.n <= 16 ? `/outputs/p16/${two(p.n)}` : null, via: "" };
      case "auxout":
        return { id: p.n >= 1 && p.n <= 6 ? `/outputs/aux/${two(p.n)}` : null, via: "" };
      case "monitor":
        return { id: this.monitor(), via: "" };
      case "userin":
      case "userout": {
        if (depth > 0 || p.n < 1) return { id: null, via: "" };
        const dir = p.kind === "userin" ? "in" : "out";
        const v = Number(this.l.value(`/config/userrout/${dir}/${two(p.n)}`) ?? 0);
        const r = this.port(userSource(v), 1);
        return { id: r.id, via: `user ${dir} ${p.n}` };
      }
    }
    return { id: null, via: "" };
  }
}

/** buildPatchbay builds the graph of the console's signal paths. */
export function buildPatchbay(l: Lookup, opts: PatchbayOptions = {}): Patchbay {
  const b = new Builder(l, opts);

  // every socket the console has, then the channels and mixes
  for (const n of range(1, b.io.ins)) b.socket("local", n);
  for (const n of range(1, 48)) b.socket("aes50a", n);
  for (const n of range(1, 48)) b.socket("aes50b", n);
  for (const n of range(1, 32)) b.socket("card", n);
  for (const n of range(1, b.io.aux)) b.socket("auxin", n);
  for (const s of STRIPS) {
    b.add({
      id: s.path,
      col: COL.strip,
      group: s.group,
      order: s.order,
      label: s.label,
      name: stripName(l, s.path),
      color: Number(l.value(`${s.path}/config/color`) ?? 0),
      muted: !on(l, `${s.path}/mix/on`),
      strip: s.path,
    });
  }
  for (const m of MIXES) {
    b.add({
      id: m.path,
      col: m.col,
      group: m.group,
      order: m.order,
      label: m.label,
      name: stripName(l, m.path),
      color: Number(l.value(`${m.path}/config/color`) ?? 0),
      muted: !on(l, `${m.path}/mix/on`),
      strip: m.path,
    });
  }

  // input routing: the 32 console inputs, then the channels and aux inputs using them
  const ins = range(1, 32).map((n) => {
    const r = b.port(inputSource(l, n));
    return { id: r.id, label: `In ${two(n)}${r.via ? " · " + r.via : ""}` };
  });
  const auxBlock = l.text(`${inputRouting(l)}/AUX`)?.trim() ?? "";
  for (const s of STRIPS) {
    if (s.path.startsWith("/fxrtn")) continue;
    const src = Number(l.value(`${s.path}/config/source`) ?? 0);
    const node = b.nodes.get(s.path)!;
    if (src >= 1 && src <= 32) b.link(ins[src - 1].id, s.path, "patch", ins[src - 1].label);
    else if (src <= 38 && src >= 33) {
      const r = b.port(auxBlockSource(auxBlock, src - 32));
      b.link(r.id, s.path, "patch", `Aux ${src - 32}${r.via ? " · " + r.via : ""}`);
    } else if (src === 39 || src === 40) b.link(b.socket("usb", src - 38), s.path, "patch", src === 39 ? "USB L" : "USB R");
    else if (src >= 41 && src <= 64) {
      // fed by an effect or a bus: back inside the console
      node.origin = true;
      node.detail = src <= 48 ? `from FX ${Math.ceil((src - 40) / 2)}${src % 2 ? "L" : "R"}` : `from Bus ${two(src - 48)}`;
    }
  }

  // sends and assigns: channels to buses and main, buses to main and matrices, main to matrices
  const tap = (strip: string, k: number) => txt(l, `${strip}/mix/${two(k % 2 ? k : k - 1)}/type`);
  const send = (strip: string, k: number, to: string) => {
    const a = `${strip}/mix/${two(k)}`;
    if (on(l, `${a}/on`) && up(l, `${a}/level`)) b.link(strip, to, "send", `${tap(strip, k)} ${txt(l, `${a}/level`)}`.trim());
  };
  const assign = (strip: string) => {
    if (on(l, `${strip}/mix/st`)) b.link(strip, "/main/st", "send", "LR");
    if (on(l, `${strip}/mix/mono`) && up(l, `${strip}/mix/mlevel`)) {
      b.link(strip, "/main/m", "send", `M/C ${txt(l, `${strip}/mix/mlevel`)}`);
    }
  };
  for (const s of STRIPS) {
    assign(s.path);
    for (const k of range(1, 16)) send(s.path, k, `/bus/${two(k)}`);
  }
  for (const n of range(1, 16)) {
    const bus = `/bus/${two(n)}`;
    assign(bus);
    for (const k of range(1, 6)) send(bus, k, `/mtx/${two(k)}`);
  }
  for (const main of ["/main/st", "/main/m"]) for (const k of range(1, 6)) send(main, k, `/mtx/${two(k)}`);

  // effects 1-4 fed by a bus (or M/C) come back on their FX returns
  for (const n of range(1, 4)) {
    (["l", "r"] as const).forEach((side, i) => {
      const src = txt(l, `/fx/${n}/source/${side}`);
      const m = /^MIX(\d+)$/.exec(src);
      const from = m ? `/bus/${two(Number(m[1]))}` : src === "M/C" ? "/main/m" : null;
      b.link(from, `/fxrtn/${two(2 * n - 1 + i)}`, "fx", `FX ${n}`);
    });
  }

  // outputs: what each one carries
  const talk = txt(l, "/config/talk/source") === "EXT" ? 2 : 1;
  for (const o of OUTPUTS) {
    for (const n of range(1, o.count)) {
      const node = `${o.node}/${two(n)}`;
      const src = Number(l.value(`${node}/src`) ?? 0);
      const s = outputSource(src);
      const from = s.strip ?? (src === 74 || src === 75 ? b.monitor() : src === 76 ? b.socket("talkback", talk) : null);
      const delay = o.node === "/outputs/main" && on(l, `${node}/delay/on`) ? `${txt(l, `${node}/delay/time`)} ms` : "";
      const pos = txt(l, `${node}/pos`);
      const side = src === 1 ? "L · " : src === 2 ? "R · " : "";
      const name = from ? stripName(l, s.strip) || s.label : "";
      b.add({ id: node, col: COL.out, group: o.group, order: o.order + n, label: o.label(n), name, detail: delay || undefined });
      b.link(from, node, "patch", side + [pos, delay].filter(Boolean).join(" · "));
    }
  }
  if (b.nodes.has("mon") && txt(l, "/config/solo/source").startsWith("LR")) b.link("/main/st", "mon", "send", "LR");

  // output ports: AES50 A and B, the card, the local XLR outputs
  const recorded = new Set(opts.recorded ?? []);
  PORTS.forEach((port, pi) => {
    const count = port.id === "OUT" ? Math.min(port.channels, b.io.outs) : port.channels;
    for (const n of range(1, count)) {
      const id = `${port.id}:${n}`;
      const aes = port.id === "AES50A" || port.id === "AES50B";
      const letter = port.id.slice(-1);
      const dev = aes ? b.device(letter as "A" | "B") : "";
      b.add({
        id,
        col: COL.port,
        group: aes ? `AES50 ${letter}${dev ? " · " + dev : ""}` : port.id === "CARD" ? "Card tracks" : "Local XLR outputs",
        order: pi * 1000 + n,
        label: aes ? `${letter} ${n}` : port.id === "CARD" ? `Track ${n}` : `XLR ${n}`,
        name: "",
        socket: aes && n <= 16 ? `box out ${n}` : undefined,
        port: port.id,
        recorded: port.id === "CARD" && recorded.has(n) ? true : undefined,
      });
      const r = b.port(portChannelSource(l, port, n));
      b.link(r.id, id, "patch", r.via);
    }
  });

  return finish(b);
}

/**
 * finish keeps the links on a path: those from a socket or a mix (a mix is
 * kept even when nothing is sent to it, as what it feeds is patched), and
 * from what they feed; then it checks the paths.
 */
function finish(b: Builder): Patchbay {
  const { nodes } = b;
  const feeds = new Set<string>();
  const out = outgoing(b.links);
  const queue = [...nodes.values()].filter((n) => n.origin || isMix(n)).map((n) => n.id);
  queue.forEach((id) => feeds.add(id));
  while (queue.length) {
    for (const k of out.get(queue.shift()!) ?? []) {
      if (!feeds.has(k.to)) {
        feeds.add(k.to);
        queue.push(k.to);
      }
    }
  }
  const links = b.links.filter((k) => feeds.has(k.from) && nodes.has(k.from) && nodes.has(k.to));
  for (const k of links) if (nodes.get(k.from)!.muted) k.muted = true;
  const into = incoming(links);
  for (const n of nodes.values()) {
    if (isMix(n) && !into.get(n.id)?.length) n.idle = true;
    if (n.col !== COL.port) continue;
    // what a port channel carries: the strip behind it
    const src = into.get(n.id)?.[0];
    const from = src && nodes.get(src.from);
    if (!from) continue;
    const ch = from.col === COL.in ? firstFed(from.id, links, nodes) : undefined;
    n.name = ch ? ch.name || ch.label : from.name || from.label;
    const via = ch ? [ch.label, ch.name].filter(Boolean).join(" ") : from.col === COL.in ? "" : from.name;
    n.detail = [from.label, via].filter(Boolean).join(" · ");
  }
  return { nodes, links, warnings: check(b, links) };
}

/** firstFed is the first channel a socket feeds. */
function firstFed(socket: string, links: PbLink[], nodes: Map<string, PbNode>): PbNode | undefined {
  for (const k of links) {
    const to = nodes.get(k.to);
    if (k.from === socket && to?.col === COL.strip) return to;
  }
  return undefined;
}

function check(b: Builder, links: PbLink[]): Warning[] {
  const { nodes, l } = b;
  const w: Warning[] = [];
  const out = outgoing(links);
  const into = incoming(links);
  const model = (b.opts.model ?? "").toUpperCase() || "this console";
  if (inputRouting(l) === "/config/routing/PLAY") {
    w.push({
      node: "",
      text: "The console's inputs come from the playback routing (PLAY): the channels hear the card, not the stage.",
    });
  }
  if (b.missing.length) {
    const who = model === "this console" ? model : "an " + model;
    const has = b.io.ins ? `has only ${b.io.ins}` : "has none";
    w.push({
      node: `in:local:${b.missing[0]}`,
      text: `Local inputs ${ranges(b.missing)} are routed, but ${who} ${has}: nothing comes from them.`,
    });
  }
  const silent: number[] = [];
  for (const n of nodes.values()) {
    if (n.recorded && !into.get(n.id)?.length) silent.push(Number(n.id.split(":")[1]));
  }
  if (silent.length) {
    const many = silent.length > 1;
    w.push({
      node: `CARD:${silent[0]}`,
      text: `Card track${many ? "s" : ""} ${ranges(silent)} ${many ? "are" : "is"} recorded but carr${many ? "y" : "ies"} nothing.`,
    });
  }
  for (const n of nodes.values()) {
    if (n.col === COL.in && n.phantom && !out.get(n.id)?.length) {
      w.push({ node: n.id, text: `48 V is on for ${n.label}${n.socket ? ` (${n.socket})` : ""}, which feeds nothing.` });
    }
    if (isMix(n) && into.get(n.id)?.length && !out.get(n.id)?.length) {
      const name = n.name ? " " + n.name : "";
      w.push({ node: n.id, text: `${n.label}${name} gets a signal but goes nowhere (no output, matrix or effect).` });
    }
  }
  return w;
}

/** ranges writes numbers as ranges: [1, 2, 3, 7] → "1-3, 7". */
export function ranges(ns: number[]): string {
  const t = [...new Set(ns)].sort((x, y) => x - y);
  const parts: string[] = [];
  for (let i = 0; i < t.length; ) {
    let j = i;
    while (j + 1 < t.length && t[j + 1] === t[j] + 1) j++;
    parts.push(j > i ? `${t[i]}-${t[j]}` : `${t[i]}`);
    i = j + 1;
  }
  return parts.join(", ");
}

function outgoing(links: PbLink[]): Map<string, PbLink[]> {
  const m = new Map<string, PbLink[]>();
  for (const k of links) m.get(k.from)?.push(k) ?? m.set(k.from, [k]);
  return m;
}

function incoming(links: PbLink[]): Map<string, PbLink[]> {
  const m = new Map<string, PbLink[]>();
  for (const k of links) m.get(k.to)?.push(k) ?? m.set(k.to, [k]);
  return m;
}

/**
 * patchbayAddrs lists the parameters the patchbay reads, to build it again
 * when one changes (and not when a fader moves).
 */
export function patchbayAddrs(): string[] {
  const a: string[] = ["/config/routing/routswitch", "/config/talk/source", "/config/solo/source", "/-stat/aes50/A", "/-stat/aes50/B"];
  for (const node of ["IN", "PLAY"]) for (const b of ["1-8", "9-16", "17-24", "25-32", "AUX"]) a.push(`/config/routing/${node}/${b}`);
  for (const p of PORTS) for (const b of p.blocks) a.push(`${p.node}/${b}`);
  for (const n of range(1, 32)) a.push(`/config/userrout/in/${two(n)}`);
  for (const n of range(1, 48)) a.push(`/config/userrout/out/${two(n)}`);
  for (const i of range(0, 127)) a.push(`${headampPath(i)}/gain`, `${headampPath(i)}/phantom`);
  const strip = (p: string, sends: number, source: boolean) => {
    a.push(`${p}/config/name`, `${p}/config/color`, `${p}/mix/on`, `${p}/mix/st`, `${p}/mix/mono`, `${p}/mix/mlevel`);
    if (source) a.push(`${p}/config/source`);
    for (const k of range(1, sends)) a.push(`${p}/mix/${two(k)}/on`, `${p}/mix/${two(k)}/level`, `${p}/mix/${two(k)}/type`);
  };
  for (const s of STRIPS) strip(s.path, 16, !s.path.startsWith("/fxrtn"));
  for (const m of MIXES) strip(m.path, m.path.startsWith("/mtx") ? 0 : 6, false);
  for (const n of range(1, 4)) a.push(`/fx/${n}/source/l`, `/fx/${n}/source/r`);
  for (const o of OUTPUTS) {
    for (const n of range(1, o.count)) {
      const p = `${o.node}/${two(n)}`;
      a.push(`${p}/src`, `${p}/pos`, `${p}/delay/on`, `${p}/delay/time`);
    }
  }
  return a;
}

// --- what to show ---

/** AES50_OUTS is how many AES50 channels a stage box plays on its outputs (16 on a DL32). */
export const AES50_OUTS = 16;

export interface ShowOptions {
  /** show the sockets, strips and port channels on no path too, and AES50 channels past 16 */
  all: boolean;
  /** the output ports to show (AES50A, AES50B, CARD, OUT); all of them when missing */
  ports?: Set<string>;
}

/**
 * defaultPorts are the output ports worth showing: AES50 B only when a
 * stage box is there (a socket of it feeds something, or the console names
 * the box).
 */
export function defaultPorts(pb: Patchbay): Set<string> {
  const ports = new Set(["AES50A", "CARD", "OUT"]);
  const used = pb.links.some((k) => k.from.startsWith("in:aes50b:"));
  const named = [...pb.nodes.values()].some((n) => n.port === "AES50B" && n.group !== "AES50 B"); // "AES50 B · DL16"
  if (used || named) ports.add("AES50B");
  return ports;
}

/** visibleNodes lists the nodes to draw, column by column in order: those on a path, or all of them. */
export function visibleNodes(pb: Patchbay, opts: ShowOptions): PbNode[] {
  const used = new Set<string>();
  for (const k of pb.links) {
    used.add(k.from);
    used.add(k.to);
  }
  const shown = (n: PbNode) => {
    if (n.port) {
      if (opts.ports && !opts.ports.has(n.port)) return false;
      const aes = n.port === "AES50A" || n.port === "AES50B";
      if (aes && !opts.all && Number(n.id.split(":")[1]) > AES50_OUTS) return false;
    }
    return opts.all || used.has(n.id) || !!n.recorded;
  };
  return [...pb.nodes.values()].filter(shown).sort((a, b) => a.col - b.col || a.order - b.order);
}

/** related lists the nodes and links upstream and downstream of a node: everything on its paths. */
export function related(pb: Patchbay, id: string): { nodes: Set<string>; links: Set<PbLink> } {
  const nodes = new Set<string>([id]);
  const links = new Set<PbLink>();
  const walk = (start: string, next: Map<string, PbLink[]>, end: (k: PbLink) => string) => {
    const seen = new Set<string>([start]);
    const queue = [start];
    while (queue.length) {
      for (const k of next.get(queue.shift()!) ?? []) {
        links.add(k);
        const n = end(k);
        nodes.add(n);
        if (!seen.has(n)) {
          seen.add(n);
          queue.push(n);
        }
      }
    }
  };
  walk(id, outgoing(pb.links), (k) => k.to);
  walk(id, incoming(pb.links), (k) => k.from);
  return { nodes, links };
}

export interface PathTree {
  node: PbNode;
  /** the link from (downstream) or to (upstream) the parent */
  link?: PbLink;
  children: PathTree[];
}

/**
 * pathTree is the tree of the paths from a node: downstream to the sockets
 * it goes out by, or upstream to the sockets it comes from, through the
 * nodes `keep` accepts (those shown). A node already on the path ends it
 * (feedback through an effect); at most `limit` nodes.
 */
export function pathTree(
  pb: Patchbay,
  id: string,
  dir: "down" | "up",
  keep: (id: string) => boolean = () => true,
  limit = 400,
): PathTree | null {
  const root = pb.nodes.get(id);
  if (!root) return null;
  const next = dir === "down" ? outgoing(pb.links) : incoming(pb.links);
  let count = 0;
  const grow = (node: PbNode, path: Set<string>, link?: PbLink): PathTree => {
    count++;
    const t: PathTree = { node, link, children: [] };
    for (const k of next.get(node.id) ?? []) {
      const n = pb.nodes.get(dir === "down" ? k.to : k.from);
      if (!n || path.has(n.id) || !keep(n.id) || count >= limit) continue;
      t.children.push(grow(n, new Set([...path, n.id]), k));
    }
    return t;
  };
  return grow(root, new Set([id]));
}

// --- layout ---

export interface Layout {
  /** top of each node, in pixels */
  y: Map<string, number>;
  /** group headings: column, title, top */
  heads: { col: Column; title: string; y: number }[];
  /** the bottom of each column */
  bottom: number[];
  height: number;
}

/** layout stacks each column's nodes under their group headings. */
export function layout(nodes: PbNode[], row: number, head: number): Layout {
  const y = new Map<string, number>();
  const heads: Layout["heads"] = [];
  const bottom = COLUMNS.map(() => 0);
  const last: (string | null)[] = COLUMNS.map(() => null);
  for (const n of nodes) {
    if (last[n.col] !== n.group) {
      if (last[n.col] !== null) bottom[n.col] += head / 3;
      heads.push({ col: n.col, title: n.group, y: bottom[n.col] });
      bottom[n.col] += head;
      last[n.col] = n.group;
    }
    y.set(n.id, bottom[n.col]);
    bottom[n.col] += row;
  }
  return { y, heads, bottom, height: Math.max(...bottom) };
}
