// The meter bridge: every level of the console at once (inputs, aux ins, FX
// returns, buses, matrices, mains, DCAs), with the gain reduction of each
// strip's gate and compressor, and a clip light that stays lit until it is
// cleared, to spot a channel that clipped while you looked elsewhere.

import { useRef, useState } from "react";
import { Bar, GrBar, useGroup } from "../components/Meter";
import { METER_CLIP, grSource, meterSource, toDbfs } from "../lib/meters";
import type { Strip } from "../lib/strips";
import { BANKS, colourOf } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useValue } from "../store/hooks";
import { NotConnected } from "./common";

const STRIPS = BANKS.flatMap((b) => b.strips);
const of = (kind: Strip["kind"]) => STRIPS.filter((s) => s.kind === kind);

/** The groups of the bridge, in the console's order. */
const GROUPS: { title: string; strips: Strip[] }[] = [
  { title: "Inputs", strips: of("ch") },
  { title: "Aux ins", strips: of("auxin") },
  { title: "FX returns", strips: of("fxrtn") },
  { title: "Mix buses", strips: of("bus") },
  { title: "Matrices", strips: of("mtx") },
  { title: "Main", strips: of("main") },
  { title: "DCAs", strips: of("dca") },
];

export function MetersPage() {
  // clearing the clip lights: each meter compares with the last clearing it saw
  const [cleared, setCleared] = useState(0);
  return (
    <div className="page meters-page">
      <div className="meters-head">
        <p className="muted">
          Every level as the console meters it, in dBFS from −60 to 0. The orange bar from the top is the gain reduction of the
          strip's gate and compressor; a red light stays lit once a meter has clipped.
        </p>
        <button type="button" className="btn" onClick={() => setCleared(cleared + 1)}>
          Clear the clip lights
        </button>
      </div>
      <NotConnected />
      <div className="meter-bridge">
        {GROUPS.map((g) => (
          <section key={g.title} className="card mb-group">
            <h3>{g.title}</h3>
            <div className="mb-row">
              {g.strips.map((s) => (
                <MeterColumn key={s.path} strip={s} cleared={cleared} />
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

/** MeterColumn is one strip on the bridge: clip light, level, gain reduction, number, name in its colour. */
function MeterColumn({ strip, cleared }: { strip: Strip; cleared: number }) {
  const src = meterSource(strip);
  const gr = grSource(strip);
  const values = useGroup(src?.group ?? "");
  const name = useValue(`${strip.path}/config/name`);
  const color = useValue(`${strip.path}/config/color`);
  // a ref, not a state: the clip is seen while rendering the meter, 20 times a second
  const clip = useRef({ lit: false, cleared });
  if (clip.current.cleared !== cleared) clip.current = { lit: false, cleared };
  if (src && values && src.indexes.some((i) => toDbfs(values[i] ?? 0) >= METER_CLIP)) clip.current.lit = true;
  const c = colourOf(color === undefined ? 0 : Number(color.value));
  const text = name ? prettyText(name.text) : "";
  const number = strip.label.replace(/^(Ch|Aux|FX|Bus|Mtx|DCA) /, ""); // "FX 1L" → "1L"
  return (
    <div className="mb-col" title={text ? `${strip.label}: ${text}` : strip.label}>
      <span className={"mb-led" + (clip.current.lit ? " lit" : "")} aria-label={clip.current.lit ? `${strip.label} clipped` : undefined} />
      <div className="mb-bars">
        {(src?.indexes ?? []).map((i) => (
          <Bar key={i} level={values?.[i]} />
        ))}
        {gr && <GrBar group={gr.group} indexes={gr.gate === undefined ? [gr.dyn] : [gr.gate, gr.dyn]} />}
      </div>
      <span className="mb-num">{number}</span>
      <span className={`mb-name c-${c.name}`}>{text || " "}</span>
    </div>
  );
}
