// The graphic EQ editor (GEQ, GEQ2, TEQ, TEQ2): 31 band faders and a
// master, side A or B of a dual EQ, with the RTA behind. Faders move by
// relative drags (a touch never makes a band jump), Shift drags finely, the
// arrow keys pick a band and move it by 0.5 dB.
//
// The RTA source is set with /-action/setrtasrc, which changes the
// console's state: it is written only when writes are armed. Otherwise the
// RTA of the current source (/-stat/rtasource) is shown, and named.

import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import { usePersistent } from "../lib/route";
import { GEQ_LABELS, RTA_BANDS, geqPosition, geqRtaSource, isDualGeq, rtaFraction, rtaFreq, rtaSourceLabel, rtaWindow } from "../lib/rta";
import { quantize, toDisplay } from "../lib/values";
import { useCanWrite, useMeterGroup, useNodes, useValue } from "../store/hooks";
import { store } from "../store/store";
import { ConfirmDialog } from "./shell";

const two = (n: number) => String(n).padStart(2, "0");
const RANGE_DB = 15; // bands and master go from -15 to +15 dB
const STEPS = 61; // in 0.5 dB steps
const GRID_DB = [12, 6, -6, -12];
const KEY_BANDS = new Set([0, 4, 7, 10, 14, 17, 20, 24, 27, 30]); // labels kept on narrow screens

const toNorm = (db: number) => quantize((db + RANGE_DB) / (2 * RANGE_DB), STEPS);
const dbText = (db: number) => (Math.round(db * 10) / 10 > 0 ? "+" : "") + (Math.round(db * 10) / 10).toFixed(1);

/** dbOf reads a band's gain in dB from the store. */
function dbOf(addr: string): number | undefined {
  const v = store.getValue(addr);
  const leaf = store.leaf(addr);
  if (!v || !leaf) return undefined;
  return toDisplay(leaf.field.type, Number(v.value));
}

export function GeqEditor({ slot, type }: { slot: number; type: string }) {
  const dual = isDualGeq(type);
  const [sideState, setSide] = useState<0 | 1>(0);
  const side = dual ? sideState : 0;
  const [post, setPost] = usePersistent("tabula.geq.post", true);
  const base = 32 * side; // parameters 01-32 (A) or 33-64 (B)
  const bands = GEQ_LABELS.map((_, i) => `/fx/${slot}/par/${two(base + i + 1)}`);
  const master = `/fx/${slot}/par/${two(base + 32)}`;
  useNodes([`/fx/${slot}/par`]);
  const can = useCanWrite(bands[0]);
  const [selected, setSelected] = useState(17);
  const [flat, setFlat] = useState(false);

  // the RTA source this editor wants, written only when writes are armed
  const want = geqRtaSource(slot, type, side as 0 | 1, post);
  const src = useValue("/-stat/rtasource");
  const current = src === undefined ? -1 : Number(src.value);
  const written = useRef(-1);
  useEffect(() => {
    if (can && current !== want && written.current !== want) {
      written.current = want;
      store.set("/-action/setrtasrc", want);
    }
    if (current === want) written.current = -1;
  }, [can, current, want]);
  const asked = useValue("/-action/setrtasrc");
  const rta = useMeterGroup("15");
  // a console that does not report /-stat/rtasource back still shows what was asked
  const showing = current === want || (written.current === want && Number(asked?.value) === want);

  const win = useRtaWindow();

  const set = (addr: string, db: number) => store.set(addr, toNorm(Math.max(-RANGE_DB, Math.min(RANGE_DB, db))));
  const selDb = dbOf(bands[selected]);

  return (
    <div className="geq-editor">
      <div className="geq-bar">
        {dual && (
          <div className="seg" role="group" aria-label="Side">
            {(["A", "B"] as const).map((s, i) => (
              <button key={s} type="button" className={"btn seg-btn" + (side === i ? " lit" : "")} aria-pressed={side === i} onClick={() => setSide(i as 0 | 1)}>
                Side {s}
              </button>
            ))}
          </div>
        )}
        <div className="seg" role="group" aria-label="RTA tap">
          {[false, true].map((p) => (
            <button key={String(p)} type="button" className={"btn seg-btn" + (post === p ? " lit" : "")} aria-pressed={post === p} onClick={() => setPost(p)}>
              RTA {p ? "after" : "before"} EQ
            </button>
          ))}
        </div>
        <span className={"geq-rta-src" + (showing ? "" : " other")}>
          {current < 0 ? "RTA: —" : `RTA: ${rtaSourceLabel(current)}`}
          {!showing && !can && " · arm writes to show this EQ"}
        </span>
      </div>

      <div className="geq">
        <GeqColumns
          addrs={bands}
          labels={GEQ_LABELS}
          selected={selected}
          onSelect={setSelected}
          disabled={!can}
          rta={rta}
          win={win}
          set={set}
          className="geq-bands"
          label="Graphic EQ bands"
        />
        <GeqColumns addrs={[master]} labels={["Master"]} selected={-1} onSelect={() => {}} disabled={!can} set={set} className="geq-master" label="Master level" />
      </div>

      <div className="geq-selected">
        <span className="geq-sel-label">
          {GEQ_LABELS[selected]} Hz{dual ? ` · side ${side ? "B" : "A"}` : ""}
        </span>
        <output className="geq-sel-value">{selDb === undefined ? "—" : dbText(selDb) + " dB"}</output>
        <button type="button" className="btn" disabled={!can || selDb === undefined} onClick={() => set(bands[selected], (selDb ?? 0) - 0.5)} aria-label="Lower by 0.5 dB">
          −
        </button>
        <button type="button" className="btn" disabled={!can || selDb === undefined} onClick={() => set(bands[selected], (selDb ?? 0) + 0.5)} aria-label="Raise by 0.5 dB">
          +
        </button>
        <button type="button" className="btn" disabled={!can || selDb === undefined} onClick={() => set(bands[selected], 0)}>
          0 dB
        </button>
        <span className="topbar-gap" />
        <button type="button" className="btn" disabled={!can} onClick={() => setFlat(true)}>
          Flat
        </button>
      </div>
      <p className="muted geq-help">
        Drag a band up or down (Shift: finely); arrow keys pick a band and move it by 0.5 dB. RTA window {win.bottom} to{" "}
        {win.top} dB, from the console's RTA settings.
      </p>
      {flat && (
        <ConfirmDialog
          title="Reset every band to 0 dB?"
          confirm="Flatten"
          onCancel={() => setFlat(false)}
          onConfirm={() => {
            setFlat(false);
            for (const b of bands) set(b, 0);
          }}
        >
          <p>
            The 31 bands of FX {slot}
            {dual ? ` side ${side ? "B" : "A"}` : ""} go back to 0 dB at once. The master level stays.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** useRtaWindow is the dB range to draw the RTA over, from the console's RTA settings (/-prefs/rta). */
export function useRtaWindow(): { top: number; bottom: number } {
  useNodes(["/-prefs/rta"]);
  const norm = (a: string) => Number(store.getValue(`/-prefs/rta/${a}`)?.value ?? 0);
  const display = (a: string, def: number) => {
    const leaf = store.leaf(`/-prefs/rta/${a}`);
    return leaf && store.getValue(leaf.node.path + "/" + a) ? toDisplay(leaf.field.type, norm(a)) : def;
  };
  return rtaWindow(display("gain", 18), norm("range") === 1, display("gain30", 30));
}

interface ColumnsProps {
  addrs: string[];
  labels: string[];
  selected: number;
  onSelect(i: number): void;
  disabled: boolean;
  set(addr: string, db: number): void;
  rta?: number[];
  win?: { top: number; bottom: number };
  className: string;
  label: string;
}

/** GeqColumns draws faders side by side and handles their drags. */
function GeqColumns({ addrs, labels, selected, onSelect, disabled, set, rta, win, className, label }: ColumnsProps) {
  const ref = useRef<HTMLDivElement>(null);
  const drag = useRef<{ i: number; y: number; db: number; h: number; id: number } | null>(null);
  const n = addrs.length;

  const indexAt = (x: number) => {
    const r = ref.current!.getBoundingClientRect();
    return Math.max(0, Math.min(n - 1, Math.floor(((x - r.left) / r.width) * n)));
  };
  const down = (e: PointerEvent<HTMLDivElement>) => {
    if (!ref.current || (e.pointerType === "mouse" && e.button !== 0)) return;
    const i = indexAt(e.clientX);
    onSelect(i);
    if (disabled) return;
    e.preventDefault();
    ref.current.setPointerCapture(e.pointerId);
    ref.current.focus({ preventScroll: true });
    drag.current = { i, y: e.clientY, db: dbOf(addrs[i]) ?? 0, h: ref.current.clientHeight, id: e.pointerId };
  };
  const move = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    const fine = e.shiftKey ? 0.2 : 1;
    d.db = Math.max(-RANGE_DB, Math.min(RANGE_DB, d.db + ((d.y - e.clientY) / d.h) * 2 * RANGE_DB * fine));
    d.y = e.clientY;
    set(addrs[d.i], d.db);
  };
  const up = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    drag.current = null;
    store.flush(addrs[d.i]);
  };
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = Math.max(0, selected);
    const step: Record<string, number> = { ArrowUp: 0.5, ArrowDown: -0.5, PageUp: 3, PageDown: -3 };
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      onSelect(Math.max(0, Math.min(n - 1, i + (e.key === "ArrowLeft" ? -1 : 1))));
    } else if (e.key in step && !disabled) {
      e.preventDefault();
      set(addrs[i], (dbOf(addrs[i]) ?? 0) + step[e.key]);
      store.flush(addrs[i]);
    }
  };

  const y = (db: number) => `${(100 * (RANGE_DB - db)) / (2 * RANGE_DB)}%`;
  const one = n === 1;
  const sel = one ? 0 : selected;
  return (
    <div className={"geq-cols " + className}>
      <div
        ref={ref}
        className={"geq-plot" + (disabled ? " disabled" : "")}
        role="slider"
        tabIndex={0}
        aria-label={label}
        aria-valuemin={-RANGE_DB}
        aria-valuemax={RANGE_DB}
        aria-valuenow={dbOf(addrs[sel]) ?? 0}
        aria-valuetext={`${labels[sel]}: ${dbText(dbOf(addrs[sel]) ?? 0)} dB`}
        aria-disabled={disabled}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        onLostPointerCapture={up}
        onKeyDown={key}
      >
        {rta && win && <RtaBars values={rta} win={win} bands={n} />}
        {GRID_DB.map((db) => (
          <div key={db} className="geq-grid" style={{ top: y(db) }} />
        ))}
        <div className="geq-zero" style={{ top: "50%" }} />
        {addrs.map((a, i) => {
          const db = dbOf(a);
          const top = db === undefined ? 50 : (100 * (RANGE_DB - Math.max(0, db))) / (2 * RANGE_DB);
          const h = db === undefined ? 0 : (100 * Math.abs(db)) / (2 * RANGE_DB);
          return (
            <div key={a} className={"geq-col" + (i === selected ? " sel" : "")} style={{ left: `${(100 * i) / n}%`, width: `${100 / n}%` }}>
              <div className={"geq-fill" + (db !== undefined && db < 0 ? " cut" : "")} style={{ top: `${top}%`, height: `${h}%` }} />
              {db !== undefined && <div className="geq-cap" style={{ top: y(db) }} />}
            </div>
          );
        })}
      </div>
      <div className="geq-labels" aria-hidden>
        {labels.map((l, i) => (
          <span key={l} className={KEY_BANDS.has(i) || one ? "key" : ""} style={{ width: `${100 / n}%` }}>
            {one ? (
              <>
                {l}
                <br />
                {dbText(dbOf(addrs[0]) ?? 0)}
              </>
            ) : (
              l
            )}
          </span>
        ))}
      </div>
    </div>
  );
}

/** RtaBars draws the 100 RTA bands on the axis of the graphic EQ bands. */
function RtaBars({ values, win, bands }: { values: number[]; win: { top: number; bottom: number }; bands: number }) {
  const half = Math.pow(2, 1 / 20); // a band spans a tenth of an octave
  const rects = [];
  for (let i = 0; i < Math.min(RTA_BANDS, values.length); i++) {
    const h = rtaFraction(values[i], win);
    if (h <= 0) continue;
    const f = rtaFreq(i);
    const x0 = Math.max(0, geqPosition(f / half));
    const x1 = Math.min(bands, geqPosition(f * half));
    if (x1 <= x0) continue;
    rects.push(<rect key={i} x={x0} y={1 - h} width={x1 - x0} height={h} />);
  }
  return (
    <svg className="geq-rta" viewBox={`0 0 ${bands} 1`} preserveAspectRatio="none" aria-hidden>
      {rects}
    </svg>
  );
}
