// Level meters fed by the "meters" events, in dBFS on a -60…0 scale with a
// peak hold, and the gain reductions of the gates and compressors (a bar
// growing down from the top). Groups beyond the default ones (the channels'
// gain reductions, the DCAs) are asked for while a meter shows them.

import { useEffect, useRef } from "react";
import { DEFAULT_GROUPS, METER_CLIP, grDb, grFraction, grSource, grText, meterFraction, meterSource, peakHold, toDbfs } from "../lib/meters";
import type { Peak } from "../lib/meters";
import type { Strip } from "../lib/strips";
import { useMeters, useValue } from "../store/hooks";
import { store } from "../store/store";

/** useGroup returns a meter group's values, asking for it while shown when the stream does not carry it already. */
export function useGroup(group: string): number[] | undefined {
  useEffect(() => (!group || DEFAULT_GROUPS.has(group) ? undefined : store.wantMeters(group)), [group]);
  return useMeters(group);
}

/**
 * StripMeter shows a strip's level, and its partner's beside it for a
 * linked pair; with gr, a thin bar of the strip's gain reduction (gate and
 * compressor together) beside them.
 */
export function StripMeter({ strip, partner, gr }: { strip: Strip; partner?: Strip; gr?: boolean }) {
  const src = meterSource(strip);
  if (!src) return <div className="meter-slot" />;
  const other = partner ? meterSource(partner) : null;
  const indexes = other && other.group === src.group ? [...src.indexes, ...other.indexes] : src.indexes;
  const reduction = gr ? grSource(strip) : null;
  return (
    <div className="meter-slot" aria-hidden>
      <MeterBars group={src.group} indexes={indexes} />
      {reduction && <GrBar group={reduction.group} indexes={reduction.gate === undefined ? [reduction.dyn] : [reduction.gate, reduction.dyn]} />}
    </div>
  );
}

function MeterBars({ group, indexes }: { group: string; indexes: number[] }) {
  const values = useGroup(group);
  return (
    <>
      {indexes.map((i) => (
        <Bar key={i} level={values?.[i]} />
      ))}
    </>
  );
}

/** Bar is one level meter. */
export function Bar({ level }: { level: number | undefined }) {
  const peak = useRef<Peak>({ db: -Infinity, at: 0 });
  const db = level === undefined ? -Infinity : toDbfs(level);
  const shown = peakHold(peak.current, db, performance.now());
  const h = meterFraction(db);
  const ph = meterFraction(shown);
  return (
    <div className={"meter" + (level === undefined ? " idle" : "")}>
      <div className="meter-fill" style={{ clipPath: `inset(${(1 - h) * 100}% 0 0 0)` }} />
      {ph > 0 && <div className={"meter-peak" + (shown >= METER_CLIP ? " clip" : "")} style={{ bottom: `${ph * 100}%` }} />}
    </div>
  );
}

/** GrBar is a thin bar of the reduction of the gains at indexes of a group, together. */
export function GrBar({ group, indexes }: { group: string; indexes: number[] }) {
  const values = useGroup(group);
  const gain = values ? indexes.reduce((g, i) => g * (values[i] ?? 1), 1) : undefined;
  const db = grDb(gain);
  return (
    <div className={"meter gr" + (values === undefined ? " idle" : "")} title={`Gain reduction ${grText(db)} dB`}>
      <div className="gr-fill" style={{ height: `${grFraction(db) * 100}%` }} />
    </div>
  );
}

/**
 * DynMeter is the meters of a gate or a compressor on the Channel page: the
 * strip's level (with the threshold marked, for a channel: its input level
 * is what the gate and compressor see) and the gain reduction, in dB.
 */
export function DynMeter({ strip, which }: { strip: Strip; which: "gate" | "dyn" }) {
  const src = grSource(strip);
  const values = useGroup(src?.group ?? "");
  const thr = useValue(`${strip.path}/${which}/thr`);
  const threshold = thr ? Number(thr.text.trim()) : undefined;
  if (!src || (which === "gate" && src.gate === undefined)) return null;
  const level = values?.[src.level];
  const db = level === undefined ? -Infinity : toDbfs(level);
  const gr = grDb(values?.[which === "gate" ? src.gate! : src.dyn]);
  const input = strip.kind === "ch";
  return (
    <div className={"dyn-meter" + (values === undefined ? " idle" : "")} aria-hidden>
      <span className="dm-label">{input ? "In" : "Level"}</span>
      <div className="hbar">
        <div className="hbar-fill" style={{ clipPath: `inset(0 ${(1 - meterFraction(db)) * 100}% 0 0)` }} />
        {input && threshold !== undefined && Number.isFinite(threshold) && threshold > -60 && (
          <div className="hbar-thr" style={{ left: `${meterFraction(threshold) * 100}%` }} title={`Threshold ${threshold} dB`} />
        )}
      </div>
      <span className="dm-value">{Number.isFinite(db) ? `${db.toFixed(0)} dB` : "-∞"}</span>
      <span className="dm-label">GR</span>
      <div className="hbar gr">
        <div className="hbar-gr" style={{ width: `${grFraction(gr) * 100}%` }} />
      </div>
      <span className="dm-value">{grText(gr) + (gr >= 0.05 ? " dB" : "")}</span>
    </div>
  );
}
