// The Routing page's Patchbay: every path a signal takes, drawn in five
// columns (the sockets it comes in by, the channels, the mixes, the outputs
// and the sockets it goes out by), to check at a glance where a stage box
// input goes and what a stage box output or a card track carries. Pointing
// at (or tapping) anything lights its paths up; a tap also spells them out.
// Read-only: the other Routing views edit the routing.

import { memo, useEffect, useMemo, useRef, useState } from "react";
import { audioApi, parseTracks } from "../lib/audio";
import { meterSource, toDbfs } from "../lib/meters";
import type { Patchbay, PathTree, PbLink, PbNode } from "../lib/patchbay";
import {
  AES50_OUTS,
  COLUMNS,
  buildPatchbay,
  defaultPorts,
  layout,
  localIO,
  pathTree,
  patchbayAddrs,
  ranges,
  related,
  visibleNodes,
} from "../lib/patchbay";
import { formatRoute, usePersistent } from "../lib/route";
import { colourOf, stripByPath } from "../lib/strips";
import { useMeters, useStatus } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";

const COL_W = 196;
const GAP = 86;
const ROW = 28;
const NODE_H = 24;
const HEAD = 26;
const WIDTH = COLUMNS.length * COL_W + (COLUMNS.length - 1) * GAP;
const colX = (c: number) => c * (COL_W + GAP);

/** useWatch counts the changes of some parameters, at most one every `ms`. */
function useWatch(addrs: string[], ms: number): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    let timer: number | undefined;
    const bump = () => {
      if (timer === undefined) {
        timer = window.setTimeout(() => {
          timer = undefined;
          setN((x) => x + 1);
        }, ms);
      }
    };
    const offs = addrs.map((a) => store.subscribe("v:" + a, bump));
    offs.push(store.subscribe("state", bump));
    return () => {
      offs.forEach((off) => off());
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [addrs, ms]);
  return n;
}

/** useRecorded reads the card tracks tabula records (none when the audio is off or not readable). */
function useRecorded(): number[] {
  const [tracks, setTracks] = useState<number[]>([]);
  useEffect(() => {
    let live = true;
    audioApi
      .get()
      .then((v) => {
        const t = v.config.enabled ? parseTracks(v.config.tracks ?? "", v.config.channels) : [];
        if (live) setTracks(typeof t === "string" ? [] : t);
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, []);
  return tracks;
}

export const PatchbayView = memo(function PatchbayView() {
  const status = useStatus();
  const addrs = useMemo(patchbayAddrs, []);
  const version = useWatch(addrs, 250);
  const recorded = useRecorded();
  const model = status.console?.info.model;
  const pb = useMemo(() => buildPatchbay(lookup, { model, recorded }), [version, model, recorded]);
  const [all, setAll] = usePersistent("tabula.patchbay.all", false);
  const [chosen, setPorts] = usePersistent<string[] | null>("tabula.patchbay.ports", null);
  const [find, setFind] = useState("");
  const [hover, setHover] = useState<string | null>(null);
  const [picked, setPicked] = useState<string | null>(null);
  const auto = useMemo(() => defaultPorts(pb), [pb]);
  const ports = useMemo(() => (chosen ? new Set(chosen) : auto), [chosen, auto]);
  const nodes = useMemo(() => visibleNodes(pb, { all, ports }), [pb, all, ports]);
  const lay = useMemo(() => layout(nodes, ROW, HEAD), [nodes]);
  const selected = picked && pb.nodes.has(picked) ? picked : null;
  const active = hover ?? selected;
  const rel = useMemo(() => (active ? related(pb, active) : null), [pb, active]);
  const matches = useMemo(() => {
    const q = find.trim().toLowerCase();
    if (!q) return null;
    return new Set(nodes.filter((n) => `${n.label} ${n.name} ${n.detail ?? ""}`.toLowerCase().includes(q)).map((n) => n.id));
  }, [nodes, find]);
  const box = useRef<HTMLDivElement>(null);

  const pick = (id: string | null, scroll = false) => {
    setPicked(id);
    if (id && scroll) {
      const port = pb.nodes.get(id)?.port;
      if (port && !ports.has(port)) setPorts([...ports, port]);
      if (!visibleNodes(pb, { all: false, ports: new Set([...ports, port ?? ""]) }).some((n) => n.id === id)) setAll(true);
      window.setTimeout(() => {
        const el = box.current?.querySelector(`[data-id="${CSS.escape(id)}"]`);
        el?.scrollIntoView({ block: "center", inline: "center", behavior: "smooth" });
      }, 50);
    }
  };
  useEffect(() => {
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setPicked(null);
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, []);

  const lit = (id: string) => (rel ? rel.nodes.has(id) : matches ? matches.has(id) : true);
  return (
    <>
      <section className={"card patchbay" + (selected ? " picked" : "")}>
        <header className="section-head">
          <h3>Patchbay</h3>
          <div className="row pb-tools">
            <div className="pb-ports" role="group" aria-label="Output ports shown">
              {PORT_CHOICES.filter((c) => c.id !== "OUT" || localIO(model).outs > 0).map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={"chip" + (ports.has(c.id) ? " active" : "")}
                  aria-pressed={ports.has(c.id)}
                  onClick={() => setPorts(ports.has(c.id) ? [...ports].filter((p) => p !== c.id) : [...ports, c.id])}
                >
                  {c.label}
                </button>
              ))}
            </div>
            <input
              className="input"
              type="search"
              value={find}
              placeholder="Find: Theo, A 12, Track 17…"
              aria-label="Find a name or a socket"
              onChange={(e) => setFind(e.target.value)}
            />
            <label className="check">
              <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
              Show unused
            </label>
          </div>
        </header>
        <p className="muted route-hint">
          Every path a signal takes, from the sockets it comes in by to those it goes out by. Point at (or tap) anything to light its
          paths up. A stage box plays AES50 channels 1-{AES50_OUTS} on its outputs: the others are under Show unused.{" "}
          <span className="pb-key patch">patch</span> <span className="pb-key send">send</span>{" "}
          <span className="pb-key fx">to an effect</span> <span className="pb-key muted">from a muted strip</span>
        </p>
        <Checks pb={pb} onPick={(id) => pick(id, true)} recorded={recorded} />
        <div className="pb-scroll" ref={box}>
          <div className="pb" style={{ width: WIDTH, height: lay.height + HEAD + 8 }}>
            {COLUMNS.map((c, i) => (
              <div key={c} className="pb-col-title" style={{ left: colX(i), width: COL_W }}>
                {c}
              </div>
            ))}
            <div className="pb-body" style={{ top: HEAD + 8 }}>
              <Links pb={pb} lay={lay.y} rel={rel?.links ?? null} />
              {lay.heads.map((h) => (
                <div key={h.col + h.title} className="pb-group" style={{ left: colX(h.col), top: h.y, width: COL_W }}>
                  {h.title}
                </div>
              ))}
              {nodes.map((n) => (
                <Node
                  key={n.id}
                  n={n}
                  top={lay.y.get(n.id)!}
                  lit={lit(n.id)}
                  dim={(!!rel || !!matches) && !lit(n.id)}
                  current={n.id === active}
                  onHover={(on) => setHover(on ? n.id : null)}
                  onPick={() => pick(selected === n.id ? null : n.id)}
                />
              ))}
              <Dots nodes={nodes} y={lay.y} />
            </div>
          </div>
        </div>
      </section>
      {selected && <Paths pb={pb} id={selected} shown={nodes} onPick={(id) => pick(id, true)} onClose={() => setPicked(null)} />}
    </>
  );
});

// --- the diagram ---

function Node(props: {
  n: PbNode;
  top: number;
  lit: boolean;
  dim: boolean;
  current: boolean;
  onHover(on: boolean): void;
  onPick(): void;
}) {
  const { n } = props;
  const c = n.color !== undefined ? colourOf(n.color) : null;
  const shown = n.col === 0 ? n.socket?.startsWith("box") ? "" : n.name : n.name;
  const cls =
    "pb-node" +
    (props.dim ? " dim" : "") +
    (props.current ? " current" : "") +
    (n.muted && n.col !== 0 ? " muted" : "") +
    (n.idle ? " idle" : "") +
    (n.missing ? " missing" : "") +
    (c ? ` c-${c.name}` : "");
  return (
    <button
      type="button"
      data-id={n.id}
      className={cls}
      style={{ left: colX(n.col), top: props.top, width: COL_W, height: NODE_H }}
      title={describe(n)}
      aria-pressed={props.current}
      onPointerEnter={(e) => e.pointerType === "mouse" && props.onHover(true)}
      onPointerLeave={(e) => e.pointerType === "mouse" && props.onHover(false)}
      onFocus={() => props.onHover(true)}
      onBlur={() => props.onHover(false)}
      onClick={props.onPick}
    >
      {c && <span className="pb-colour" />}
      <span className="pb-label">{n.label}</span>
      <span className="pb-name">{shown}</span>
      {n.col === 0 && n.detail && <span className="pb-detail">{n.detail.replace(" dB", "")}</span>}
      {n.col === 3 && n.detail && <span className="pb-detail">{n.detail}</span>}
      {n.col === 4 && n.socket && <span className="pb-detail">{n.socket.replace("box ", "")}</span>}
      {n.phantom && <span className="pb-tag phantom">48V</span>}
      {n.recorded && <span className="pb-tag rec">REC</span>}
      {n.muted && n.col !== 0 && <span className="pb-tag mute">M</span>}
    </button>
  );
}

const PORT_CHOICES = [
  { id: "AES50A", label: "AES50 A" },
  { id: "AES50B", label: "AES50 B" },
  { id: "CARD", label: "Card" },
  { id: "OUT", label: "XLR outs" },
];

/** describe says all about a node, for its tooltip. */
function describe(n: PbNode): string {
  const carries = n.col === 4 ? n.detail : n.name;
  const parts = [n.label, n.socket, n.col === 4 ? "" : n.name, n.col === 4 ? carries : n.detail];
  parts.push(n.phantom ? "48 V" : "", n.muted ? "muted" : "", n.idle ? "nothing is sent to it" : "");
  if (n.recorded) parts.push("recorded with the sessions");
  if (n.missing) parts.push("not on this console");
  return parts.filter(Boolean).join(" · ");
}

function Links({ pb, lay, rel }: { pb: Patchbay; lay: Map<string, number>; rel: Set<PbLink> | null }) {
  const height = Math.max(0, ...lay.values()) + ROW;
  const drawn = pb.links.filter((k) => lay.has(k.from) && lay.has(k.to));
  // the lit links last, so they are drawn over the others
  const sorted = rel ? [...drawn.filter((k) => !rel.has(k)), ...drawn.filter((k) => rel.has(k))] : drawn;
  return (
    <svg className="pb-links" width={WIDTH} height={height} aria-hidden>
      {sorted.map((k, i) => {
        const from = pb.nodes.get(k.from)!;
        const to = pb.nodes.get(k.to)!;
        const cls = "pb-link " + k.kind + (k.muted ? " muted" : "") + (rel ? (rel.has(k) ? " on" : " off") : "");
        return <path key={i} className={cls} d={curve(from, to, lay.get(k.from)!, lay.get(k.to)!)} />;
      })}
    </svg>
  );
}

/** curve draws a link: forward between columns, back for an effect return, or an arc within the mixes. */
function curve(from: PbNode, to: PbNode, yf: number, yt: number): string {
  const y1 = yf + NODE_H / 2;
  const y2 = yt + NODE_H / 2;
  if (to.col > from.col) {
    const x1 = colX(from.col) + COL_W;
    const x2 = colX(to.col);
    const dx = Math.max(30, Math.min(GAP, (x2 - x1) / 2));
    return `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`;
  }
  if (to.col === from.col) {
    const x = colX(from.col) + COL_W;
    const r = Math.min(GAP * 0.8, 16 + Math.abs(y2 - y1) * 0.12);
    return `M${x},${y1} C${x + r},${y1} ${x + r},${y2} ${x},${y2}`;
  }
  const x1 = colX(from.col);
  const x2 = colX(to.col) + COL_W;
  return `M${x1},${y1} C${x1 - GAP / 2},${y1} ${x2 + GAP / 2},${y2} ${x2},${y2}`;
}

/** Dots light while a strip has a signal (its meter above −50 dBFS). */
function Dots({ nodes, y }: { nodes: PbNode[]; y: Map<string, number> }) {
  const m0 = useMeters("0");
  const m2 = useMeters("2");
  return (
    <>
      {nodes.map((n) => {
        const s = n.strip ? stripByPath(n.strip) : undefined;
        const src = s ? meterSource(s) : null;
        if (!src) return null;
        const values = src.group === "0" ? m0 : m2;
        const level = Math.max(...src.indexes.map((i) => values?.[i] ?? 0));
        const db = toDbfs(level);
        const cls = db > -1 ? " clip" : db > -50 ? " sig" : "";
        return (
          <span
            key={n.id}
            className={"pb-dot" + cls}
            style={{ left: colX(n.col) + COL_W - 9, top: y.get(n.id)! + NODE_H / 2 - 3 }}
            aria-hidden
          />
        );
      })}
    </>
  );
}

// --- the checks ---

function Checks({ pb, onPick, recorded }: { pb: Patchbay; onPick(id: string): void; recorded: number[] }) {
  const rec = recorded.length > 0 && <span className="muted"> Card tracks recorded with the sessions: {ranges(recorded)}.</span>;
  return (
    <div className="pb-checks">
      {pb.warnings.length ? (
        <ul className="pb-warnings">
          {pb.warnings.map((w, i) => (
            <li key={i}>
              {w.node ? (
                <button type="button" className="pb-warn-link" onClick={() => onPick(w.node)}>
                  {w.text}
                </button>
              ) : (
                w.text
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="pb-ok">✓ No problem found on the paths.{rec}</p>
      )}
      {pb.warnings.length > 0 && rec && <p className="pb-ok">{rec}</p>}
    </div>
  );
}

// --- the paths of the node picked ---

function Paths(props: { pb: Patchbay; id: string; shown: PbNode[]; onPick(id: string): void; onClose(): void }) {
  const { pb, id, onPick, onClose } = props;
  const n = pb.nodes.get(id)!;
  const shown = useMemo(() => new Set(props.shown.map((s) => s.id)), [props.shown]);
  const keep = (k: string) => shown.has(k);
  const up = pathTree(pb, id, "up", keep);
  const down = pathTree(pb, id, "down", keep);
  const strip = n.strip && /^\/(ch|auxin|fxrtn|bus|mtx|main)\//.test(n.strip) ? n.strip : null;
  return (
    <aside className="pb-paths" aria-label={`Paths of ${n.label}`}>
      <header className="section-head">
        <h3>
          {n.label}
          {n.name && <span className="muted"> {n.name}</span>}
          {n.socket && <span className="muted"> · {n.socket}</span>}
          {n.phantom && <span className="badge phantom">48 V</span>}
          {n.recorded && <span className="badge">recorded</span>}
          {n.muted && n.col !== 0 && <span className="badge">muted</span>}
        </h3>
        <div className="row">
          {strip && (
            <a className="btn" href={formatRoute({ page: "channel", strip })}>
              Open the strip
            </a>
          )}
          <button type="button" className="btn" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
      </header>
      {n.detail && <p className="muted pb-path-detail">{n.detail}</p>}
      <div className="pb-path-cols">
        <div>
          <h4>Comes from</h4>
          {up?.children.length ? (
            <Tree t={up} dir="up" onPick={onPick} />
          ) : (
            <p className="muted">{n.col === 0 ? "A socket: the signal comes in here." : n.idle ? "Nothing is sent to it." : "Nothing."}</p>
          )}
        </div>
        <div>
          <h4>Goes to</h4>
          {down?.children.length ? (
            <Tree t={down} dir="down" onPick={onPick} />
          ) : (
            <p className="muted">{n.col === 4 ? "A socket: the signal goes out here." : "Nowhere."}</p>
          )}
        </div>
      </div>
    </aside>
  );
}

function Tree({ t, dir, onPick }: { t: PathTree; dir: "up" | "down"; onPick(id: string): void }) {
  return (
    <ul className="pb-tree">
      {t.children.map((c, i) => (
        <Branch key={i} t={c} dir={dir} onPick={onPick} />
      ))}
    </ul>
  );
}

/** Branch writes a path on one line while it does not fork; leaves that end it together share the line. */
function Branch({ t, dir, onPick }: { t: PathTree; dir: "up" | "down"; onPick(id: string): void }) {
  const chain = [t];
  let last = t;
  while (last.children.length === 1) {
    last = last.children[0];
    chain.push(last);
  }
  const leaves = last.children.length > 1 && last.children.every((c) => !c.children.length);
  return (
    <li>
      {chain.map((c, i) => (
        <span key={i} className="pb-step">
          <Arrow link={c.link} dir={dir} />
          <Chip n={c.node} onPick={onPick} />
        </span>
      ))}
      {leaves ? (
        <span className="pb-step">
          <Arrow link={last.children[0].link} dir={dir} />
          {last.children.map((c, i) => (
            <span key={i}>
              {i > 0 && ", "}
              <Chip n={c.node} onPick={onPick} />
            </span>
          ))}
        </span>
      ) : (
        last.children.length > 1 && <Tree t={last} dir={dir} onPick={onPick} />
      )}
    </li>
  );
}

function Arrow({ link, dir }: { link?: PbLink; dir: "up" | "down" }) {
  return (
    <span className={"pb-arrow" + (link?.muted ? " muted" : "")}>
      {dir === "down" ? "→" : "←"}
      {link?.label && <small>{link.label}</small>}
    </span>
  );
}

function Chip({ n, onPick }: { n: PbNode; onPick(id: string): void }) {
  const c = n.color !== undefined ? colourOf(n.color) : null;
  return (
    <button type="button" className={"pb-chip" + (c ? ` c-${c.name}` : "")} title={describe(n)} onClick={() => onPick(n.id)}>
      {n.label}
      {n.name && n.col !== 4 && <strong> {n.name}</strong>}
      {n.socket?.startsWith("box") && <span className="pb-socket"> {n.socket}</span>}
      {n.phantom && <span className="pb-tag phantom">48V</span>}
      {n.recorded && <span className="pb-tag rec">REC</span>}
    </button>
  );
}
