// The EQ frequency response of a strip, drawn from its band values. The
// curve is approximate (see lib/eq.ts).

import type { Band } from "../lib/eq";
import { butterworthDb, logFrequencies, response } from "../lib/eq";
import { toDisplay } from "../lib/values";
import { useMeterGroup, useNodes } from "../store/hooks";
import { store } from "../store/store";
import { rtaRects } from "./RtaBars";

const W = 900;
const H = 240;
const PAD_L = 34;
const PAD_B = 18;
const RANGE_DB = 18;
const F_LO = 20;
const F_HI = 20000;
const FREQS = logFrequencies(240, F_LO, F_HI);
const GRID_F = [50, 100, 200, 500, 1000, 2000, 5000, 10000];
const GRID_DB = [-12, -6, 0, 6, 12];

const x = (f: number) => PAD_L + ((W - PAD_L) * Math.log(f / F_LO)) / Math.log(F_HI / F_LO);
const y = (db: number) => {
  const clamped = Math.max(-RANGE_DB, Math.min(RANGE_DB, db));
  return ((H - PAD_B) * (RANGE_DB - clamped)) / (2 * RANGE_DB);
};

/** readBand reads one EQ band (type, f, g, q) from the store. */
function readBand(path: string, index: number, count: number): Band | null {
  const num = (name: string) => {
    const leaf = store.leaf(`${path}/${name}`);
    const v = store.getValue(`${path}/${name}`);
    return leaf && v ? toDisplay(leaf.field.type, Number(v.value)) : NaN;
  };
  const typeLeaf = store.leaf(`${path}/type`);
  const tv = store.getValue(`${path}/type`);
  if (!typeLeaf || !tv) return null;
  const band: Band = {
    type: typeLeaf.field.type.names?.[Number(tv.value)] ?? "PEQ",
    freq: num("f"),
    gain: num("g"),
    q: num("q"),
    highPass: index < count / 2,
  };
  return Number.isFinite(band.freq) ? band : null;
}

export function EqCurve(props: {
  strip: string;
  bands: string[];
  eqOn: boolean;
  /** the dB range of the RTA, drawn behind when the RTA listens to this strip */
  rtaWindow?: { top: number; bottom: number };
}) {
  const { strip, bands, eqOn } = props;
  useNodes([...bands, `${strip}/preamp`]);
  const read = bands.map((b, i) => readBand(b, i, bands.length));
  const valid = read.filter((b): b is Band => b !== null);

  // the preamp low cut, when the strip has one and it is on
  const extra: ((f: number) => number)[] = [];
  const hpon = store.getValue(`${strip}/preamp/hpon`);
  if (hpon && Number(hpon.value) === 1) {
    const slope = store.leaf(`${strip}/preamp/hpslope`);
    const sv = store.getValue(`${strip}/preamp/hpslope`);
    const order = Number(slope?.field.type.names?.[Number(sv?.value)] ?? 12) / 6;
    const fl = store.leaf(`${strip}/preamp/hpf`);
    const fv = store.getValue(`${strip}/preamp/hpf`);
    if (fl && fv) {
      const fc = toDisplay(fl.field.type, Number(fv.value));
      extra.push((f) => butterworthDb(f, fc, order, true));
    }
  }

  const total = response(eqOn ? valid : [], FREQS, extra);
  const path = FREQS.map((f, i) => `${i ? "L" : "M"}${x(f).toFixed(1)},${y(total[i]).toFixed(1)}`).join("");
  const fill = `${path}L${x(F_HI)},${y(0)}L${x(F_LO)},${y(0)}Z`;
  return (
    <figure className="eqcurve">
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label="EQ frequency response (approximate)">
        <rect x={PAD_L} y={0} width={W - PAD_L} height={H - PAD_B} className="eq-bg" />
        {props.rtaWindow && <EqRta win={props.rtaWindow} />}
        {GRID_F.map((f) => (
          <g key={f}>
            <line x1={x(f)} x2={x(f)} y1={0} y2={H - PAD_B} className="eq-grid" />
            <text x={x(f)} y={H - 4} className="eq-axis" textAnchor="middle">
              {f >= 1000 ? `${f / 1000}k` : f}
            </text>
          </g>
        ))}
        {GRID_DB.map((db) => (
          <g key={db}>
            <line x1={PAD_L} x2={W} y1={y(db)} y2={y(db)} className={db === 0 ? "eq-zero" : "eq-grid"} />
            <text x={PAD_L - 4} y={y(db) + 4} className="eq-axis" textAnchor="end">
              {db > 0 ? `+${db}` : db}
            </text>
          </g>
        ))}
        <path d={fill} className={"eq-fill" + (eqOn ? "" : " off")} />
        <path d={path} className={"eq-line" + (eqOn ? "" : " off")} />
        {eqOn &&
          read.map((b, i) =>
            b ? (
              <g key={i} className="eq-handle">
                <circle cx={x(b.freq)} cy={y(response(valid, [b.freq], extra)[0])} r={9} />
                <text x={x(b.freq)} y={y(response(valid, [b.freq], extra)[0]) + 4} textAnchor="middle">
                  {i + 1}
                </text>
              </g>
            ) : null,
          )}
      </svg>
      <figcaption className="muted">
        Approximate response (standard biquads at 48 kHz{extra.length ? ", including the preamp low cut" : ""}).
        {!eqOn && " EQ is off."}
        {props.rtaWindow && ` RTA behind, ${props.rtaWindow.bottom} to ${props.rtaWindow.top} dB.`}
      </figcaption>
    </figure>
  );
}

/** EqRta draws the RTA bands behind the curve: it alone re-renders with them, not the curve or the band editors. */
function EqRta({ win }: { win: { top: number; bottom: number } }) {
  const rta = useMeterGroup("15");
  return rta ? <g className="eq-rta">{rtaRects(rta, win, x, PAD_L, W, H - PAD_B)}</g> : null;
}
