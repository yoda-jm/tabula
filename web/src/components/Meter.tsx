// Level meters fed by the "meters" events, in dBFS on a -60…0 scale with a
// peak hold.

import { useRef } from "react";
import { meterFraction, meterSource, peakHold, toDbfs } from "../lib/meters";
import type { Peak } from "../lib/meters";
import type { Strip } from "../lib/strips";
import { useMeters } from "../store/hooks";

/** StripMeter shows a strip's level, and its partner's beside it for a linked pair. */
export function StripMeter({ strip, partner }: { strip: Strip; partner?: Strip }) {
  const src = meterSource(strip);
  if (!src) return <div className="meter-slot" />;
  const other = partner ? meterSource(partner) : null;
  const indexes = other && other.group === src.group ? [...src.indexes, ...other.indexes] : src.indexes;
  return <MeterBars group={src.group} indexes={indexes} />;
}

function MeterBars({ group, indexes }: { group: string; indexes: number[] }) {
  const values = useMeters(group);
  return (
    <div className="meter-slot" aria-hidden>
      {indexes.map((i) => (
        <Bar key={i} level={values?.[i]} />
      ))}
    </div>
  );
}

function Bar({ level }: { level: number | undefined }) {
  const peak = useRef<Peak>({ db: -Infinity, at: 0 });
  const db = level === undefined ? -Infinity : toDbfs(level);
  const shown = peakHold(peak.current, db, performance.now());
  const h = meterFraction(db);
  const ph = meterFraction(shown);
  return (
    <div className={"meter" + (level === undefined ? " idle" : "")}>
      <div className="meter-fill" style={{ clipPath: `inset(${(1 - h) * 100}% 0 0 0)` }} />
      {ph > 0 && <div className={"meter-peak" + (shown >= -0.5 ? " clip" : "")} style={{ bottom: `${ph * 100}%` }} />}
    </div>
  );
}
