// The EQ frequency response of a strip, drawn from its band values, with a
// handle per band to drag: sideways for the frequency, up and down for the
// gain (bands that have one); Page Up/Down, or the wheel over the selected
// handle, change its Q. The preamp low cut, when on, has a handle too
// (sideways only). Drags are relative, so a touch never makes a band jump;
// Shift drags finely; the arrow keys move a focused handle. What is out of
// the ±18 dB in view (the slope of a low cut) runs out of the drawing. The
// curve is approximate (see lib/eq.ts).

import { useEffect, useId, useRef, useState } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import type { Band, Plot } from "../lib/eq";
import { butterworthDb, hasGain, logFrequencies, plotDb, plotFreq, plotX, plotY, response } from "../lib/eq";
import { fromDisplay, prettyText, stepSize, toDisplay } from "../lib/values";
import { useCanWrite, useMeterGroup, useNodes } from "../store/hooks";
import { store } from "../store/store";
import { rtaRects } from "./RtaBars";

const W = 900;
const FREQS = logFrequencies(240, 20, 20000);
const GRID_F = [50, 100, 200, 500, 1000, 2000, 5000, 10000];
const GRID_DB = [-12, -6, 0, 6, 12];

/** Geo is the drawing's geometry for a height: wide on a screen, taller on a phone (to drag the handles). */
interface Geo {
  h: number;
  plot: Plot;
  x(f: number): number;
  y(db: number): number;
}

/** geo is the geometry of a drawing h high, leaving left for the dB scale and bottom for the frequencies. */
function geo(h: number, left: number, bottom: number): Geo {
  const plot: Plot = { left, right: W, top: 0, bottom: h - bottom, fLo: 20, fHi: 20000, range: 18 };
  return {
    h,
    plot,
    x: (f) => plotX(plot, f),
    // out of view, the curve goes on out of the plot (clipped); far points are kept near it
    y: (db) => plotY(plot, Math.max(-3 * plot.range, Math.min(3 * plot.range, db))),
  };
}

const WIDE = geo(240, 34, 18);
const TALL = geo(440, 60, 30);
const NARROW = 640; // CSS pixels: narrower, the drawing is taller

// a handle stays in view
const inView = (db: number) => Math.max(-18 + 1.5, Math.min(18 - 1.5, db));

/** display reads a parameter as displayed (Hz, dB…), NaN if unknown. */
function display(addr: string): number {
  const leaf = store.leaf(addr);
  const v = store.getValue(addr);
  return leaf && v ? toDisplay(leaf.field.type, Number(v.value)) : NaN;
}

/** text is a parameter's value as the fields show it. */
const text = (addr: string) => prettyText(store.getValue(addr)?.text);

/** readBand reads one EQ band (type, f, g, q) from the store. */
function readBand(path: string, index: number, count: number): Band | null {
  const typeLeaf = store.leaf(`${path}/type`);
  const tv = store.getValue(`${path}/type`);
  if (!typeLeaf || !tv) return null;
  const band: Band = {
    type: typeLeaf.field.type.names?.[Number(tv.value)] ?? "PEQ",
    freq: display(`${path}/f`),
    gain: display(`${path}/g`),
    q: display(`${path}/q`),
    highPass: index < count / 2,
  };
  return Number.isFinite(band.freq) ? band : null;
}

/** write sets a parameter to a displayed value, within its range and steps; false if that changes nothing. */
function write(addr: string, value: number): boolean {
  const t = store.leaf(addr)?.field.type;
  if (!t) return false;
  const lo = Math.min(t.min ?? value, t.max ?? value);
  const hi = Math.max(t.min ?? value, t.max ?? value);
  const n = fromDisplay(t, Math.max(lo, Math.min(hi, value)));
  if (Number(store.getValue(addr)?.value) === n) return false;
  store.set(addr, n);
  return true;
}

/** step moves a parameter by a number of its steps (its normalized value goes 0 to 1). */
function step(addr: string, steps: number) {
  const t = store.leaf(addr)?.field.type;
  const v = store.getValue(addr);
  if (!t || !v) return;
  const n = Math.max(0, Math.min(1, Number(v.value) + steps * (stepSize(t.steps) || 0.01)));
  store.set(addr, fromDisplay(t, toDisplay(t, n)));
  store.flush(addr);
}

interface Handle {
  key: string; // "1"…"6" for the bands, "lc" for the low cut
  label: string; // inside the dot
  name: string; // "Band 2", "Low cut"
  f: string; // addresses of its frequency, gain and Q
  g?: string;
  q?: string;
  freq: number;
  db: number; // where the dot is
  readout: string;
  dim: boolean;
}

export function EqCurve(props: {
  strip: string;
  bands: string[];
  eqOn: boolean;
  /** the dB range of the RTA, drawn behind when the RTA listens to this strip */
  rtaWindow?: { top: number; bottom: number };
  /** the handle selected ("1"…, "lc"), and its setter */
  selected?: string | null;
  onSelect?(key: string | null): void;
}) {
  const { strip, bands, eqOn, selected, onSelect } = props;
  useNodes([...bands, `${strip}/preamp`]);
  const can = useCanWrite(bands[0] ? `${bands[0]}/f` : undefined);
  const svgRef = useRef<SVGSVGElement>(null);
  const clip = "eqclip" + useId().replace(/:/g, "");
  const [scale, setScale] = useState(1); // CSS pixels per drawing unit
  const [tall, setTall] = useState(false);
  const g = tall ? TALL : WIDE;
  const { x, y, plot } = g;
  const drag = useRef<{ h: Handle; id: number; px: number; py: number; hx: number; hy: number; wrote: Set<string> } | null>(null);

  const read = bands.map((b, i) => readBand(b, i, bands.length));
  const valid = read.filter((b): b is Band => b !== null);

  // the preamp low cut, when the strip has one and it is on
  const extra: ((f: number) => number)[] = [];
  const hpf = `${strip}/preamp/hpf`;
  const lowCut = Number(store.getValue(`${strip}/preamp/hpon`)?.value) === 1 ? display(hpf) : NaN;
  if (Number.isFinite(lowCut)) {
    const slope = store.leaf(`${strip}/preamp/hpslope`);
    const sv = store.getValue(`${strip}/preamp/hpslope`);
    const order = Number(slope?.field.type.names?.[Number(sv?.value)] ?? 12) / 6;
    extra.push((f) => butterworthDb(f, lowCut, order, true));
  }

  const shown = eqOn ? valid : [];
  const at = (f: number) => response(shown, [f], extra)[0];
  const total = response(shown, FREQS, extra);
  const path = FREQS.map((f, i) => `${i ? "L" : "M"}${x(f).toFixed(1)},${y(total[i]).toFixed(1)}`).join("");
  const fill = `${path}L${x(plot.fHi)},${y(0)}L${x(plot.fLo)},${y(0)}Z`;

  const handles: Handle[] = [];
  read.forEach((b, i) => {
    if (!b) return;
    const p = bands[i];
    const gain = hasGain(b.type);
    handles.push({
      key: String(i + 1),
      label: String(i + 1),
      name: `Band ${i + 1}`,
      f: `${p}/f`,
      g: gain ? `${p}/g` : undefined,
      q: gain ? `${p}/q` : undefined,
      freq: b.freq,
      db: inView(gain && Number.isFinite(b.gain) ? b.gain : at(b.freq)),
      readout: [b.type, `${text(`${p}/f`)} Hz`, gain && `${text(`${p}/g`)} dB`, gain && `Q ${text(`${p}/q`)}`].filter(Boolean).join(" · "),
      dim: !eqOn,
    });
  });
  if (Number.isFinite(lowCut)) {
    handles.push({
      key: "lc",
      label: "LC",
      name: "Low cut",
      f: hpf,
      freq: lowCut,
      db: inView(at(lowCut)),
      readout: `${text(hpf)} Hz · ${text(`${strip}/preamp/hpslope`)} dB/oct`,
      dim: false,
    });
  }
  // the selected handle on top
  handles.sort((a, b) => Number(a.key === selected) - Number(b.key === selected));
  const sel = handles.find((h) => h.key === selected);

  // the size of a CSS pixel in the drawing: handles keep a usable size on a phone
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      const r = svg.getBoundingClientRect();
      if (!r.width) return;
      const t = r.width < NARROW;
      setTall(t);
      setScale(Math.min(r.width / W, r.height / (t ? TALL : WIDE).h));
    });
    ro.observe(svg);
    return () => ro.disconnect();
  }, []);

  // the wheel over the selected handle changes its Q, and a touch on a handle drags it: neither scrolls the page
  // (listeners that are not passive; touch-action does not reach into an SVG everywhere)
  const live = useRef<{ can: boolean; sel?: Handle }>({ can });
  live.current = { can, sel };
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const over = (e: Event) => (e.target as Element).closest?.("[data-handle]")?.getAttribute("data-handle");
    const wheel = (e: WheelEvent) => {
      const { can, sel } = live.current;
      if (!can || !sel?.q || over(e) !== sel.key || !e.deltaY) return;
      e.preventDefault();
      step(sel.q, e.deltaY < 0 ? -1 : 1); // Q runs from 10 (0) down to 0.3 (1): up is narrower
    };
    const touch = (e: TouchEvent) => {
      if (live.current.can && over(e)) e.preventDefault();
    };
    svg.addEventListener("wheel", wheel, { passive: false });
    svg.addEventListener("touchstart", touch, { passive: false });
    return () => {
      svg.removeEventListener("wheel", wheel);
      svg.removeEventListener("touchstart", touch);
    };
  }, []);

  const toSvg = (e: { clientX: number; clientY: number }) => {
    const m = svgRef.current?.getScreenCTM();
    return m ? new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse()) : null;
  };
  const down = (h: Handle) => (e: PointerEvent<SVGGElement>) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    e.stopPropagation();
    onSelect?.(h.key);
    const p = toSvg(e);
    if (!can || !p || !svgRef.current) return;
    e.preventDefault();
    svgRef.current.setPointerCapture(e.pointerId);
    (e.currentTarget as SVGGElement).focus({ preventScroll: true });
    drag.current = { h, id: e.pointerId, px: p.x, py: p.y, hx: x(h.freq), hy: plotY(plot, h.db), wrote: new Set() };
  };
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const d = drag.current;
    const p = d && d.id === e.pointerId ? toSvg(e) : null;
    if (!d || !p) return;
    const fine = e.shiftKey ? 0.2 : 1;
    d.hx = Math.max(plot.left, Math.min(plot.right, d.hx + (p.x - d.px) * fine));
    d.hy = Math.max(plot.top, Math.min(plot.bottom, d.hy + (p.y - d.py) * fine));
    d.px = p.x;
    d.py = p.y;
    if (write(d.h.f, plotFreq(plot, d.hx))) d.wrote.add(d.h.f);
    if (d.h.g && write(d.h.g, plotDb(plot, d.hy))) d.wrote.add(d.h.g);
  };
  const up = (e: PointerEvent<SVGSVGElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    drag.current = null;
    for (const a of d.wrote) store.flush(a);
  };
  const key = (h: Handle) => (e: KeyboardEvent<SVGGElement>) => {
    if (!can) return;
    const n = e.shiftKey ? 1 : 4; // steps: f has 201 (1/20 octave each), g 121 (0.25 dB)
    const moves: Record<string, [string | undefined, number]> = {
      ArrowLeft: [h.f, -n],
      ArrowRight: [h.f, n],
      ArrowUp: [h.g, n],
      ArrowDown: [h.g, -n],
      PageUp: [h.q, -1],
      PageDown: [h.q, 1],
    };
    const [addr, by] = moves[e.key] ?? [];
    if (!addr || !by) return;
    e.preventDefault();
    onSelect?.(h.key);
    step(addr, by);
  };

  const r = Math.max(9, 8 / scale);
  const hit = Math.max(22, 22 / scale);
  const font = Math.max(11, 10 / scale);
  const axis = Math.max(11, 9 / scale);
  return (
    <figure className="eqcurve">
      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${g.h}`}
        role="group"
        aria-label="EQ frequency response (approximate)"
        onPointerDown={() => onSelect?.(null)}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        onLostPointerCapture={up}
      >
        <defs>
          <clipPath id={clip}>
            <rect x={plot.left} y={plot.top} width={plot.right - plot.left} height={plot.bottom - plot.top} />
          </clipPath>
        </defs>
        <rect x={plot.left} y={plot.top} width={plot.right - plot.left} height={plot.bottom - plot.top} className="eq-bg" />
        {props.rtaWindow && <EqRta win={props.rtaWindow} g={g} />}
        {GRID_F.map((f) => (
          <g key={f}>
            <line x1={x(f)} x2={x(f)} y1={plot.top} y2={plot.bottom} className="eq-grid" />
            <text x={x(f)} y={g.h - 4} className="eq-axis" textAnchor="middle" style={{ fontSize: axis }}>
              {f >= 1000 ? `${f / 1000}k` : f}
            </text>
          </g>
        ))}
        {GRID_DB.map((db) => (
          <g key={db}>
            <line x1={plot.left} x2={plot.right} y1={y(db)} y2={y(db)} className={db === 0 ? "eq-zero" : "eq-grid"} />
            <text x={plot.left - 4} y={y(db) + axis * 0.36} className="eq-axis" textAnchor="end" style={{ fontSize: axis }}>
              {db > 0 ? `+${db}` : db}
            </text>
          </g>
        ))}
        <g clipPath={`url(#${clip})`}>
          <path d={fill} className={"eq-fill" + (eqOn ? "" : " off")} />
          <path d={path} className={"eq-line" + (eqOn ? "" : " off")} />
        </g>
        {sel && (
          <text x={plot.left + 8} y={plot.top + font + 4} className="eq-readout" style={{ fontSize: font }}>
            {sel.name} · {sel.readout}
          </text>
        )}
        {handles.map((h) => (
          <g
            key={h.key}
            data-handle={h.key}
            className={"eq-handle" + (h.key === selected ? " sel" : "") + (h.dim ? " dim" : "") + (h.key === "lc" ? " lc" : "") + (can ? "" : " ro")}
            role="button"
            tabIndex={can ? 0 : -1}
            aria-label={`${h.name}: ${h.readout}`}
            aria-pressed={h.key === selected}
            onPointerDown={down(h)}
            onKeyDown={key(h)}
            onFocus={() => onSelect?.(h.key)}
          >
            <circle cx={x(h.freq)} cy={plotY(plot, h.db)} r={hit} className="eq-hit" />
            <circle cx={x(h.freq)} cy={plotY(plot, h.db)} r={h.key === "lc" ? r * 1.15 : r} />
            <text x={x(h.freq)} y={plotY(plot, h.db) + font * 0.36} textAnchor="middle" style={{ fontSize: h.key === "lc" ? font * 0.85 : font }}>
              {h.label}
            </text>
          </g>
        ))}
      </svg>
      <figcaption className="muted">
        Approximate response (standard biquads at 48 kHz{extra.length ? ", including the preamp low cut" : ""}).
        {!eqOn && " EQ is off."}
        {props.rtaWindow && ` RTA behind, ${props.rtaWindow.bottom} to ${props.rtaWindow.top} dB.`}
        {can && " Drag a handle: sideways for the frequency, up and down for the gain; Shift drags finely; the wheel or Page Up/Down changes the selected band's Q."}
      </figcaption>
    </figure>
  );
}

/** EqRta draws the RTA bands behind the curve: it alone re-renders with them, not the curve or the band editors. */
function EqRta({ win, g }: { win: { top: number; bottom: number }; g: Geo }) {
  const rta = useMeterGroup("15");
  return rta ? <g className="eq-rta">{rtaRects(rta, win, g.x, g.plot.left, W, g.plot.bottom)}</g> : null;
}
