// The FX page's tap tempo: tapped (button, T key), typed, or taken from the
// song of the setlist, a tempo sets the slot's delay times and modulation
// speeds to a note value of it (lib/tempo).

import { useEffect, useRef, useState } from "react";
import { songLabel, songTempo } from "../lib/setlist";
import type { Target } from "../lib/tempo";
import {
  MAX_BPM,
  MIN_BPM,
  NOTES,
  addTap,
  beatMs,
  bpmOf,
  formatBpm,
  isTapKey,
  noteBeats,
  parseBpm,
  targetValue,
  tempoOfValue,
  tempoTargets,
} from "../lib/tempo";
import { formatValue, fromDisplay, toDisplay } from "../lib/values";
import { useCanWrite, useNode, useSetlist } from "../store/hooks";
import { store } from "../store/store";

interface Row extends Target {
  note: string;
}

/** TapTempo is the tempo card of an FX slot; the parent keys it by effect, so another effect starts afresh. */
export function TapTempo({ slot, type }: { slot: number; type: string }) {
  const path = `/fx/${slot}/par`;
  useNode(path); // the values shown follow the console
  // the targets come from the effect's fields as they are now (they arrive
  // a moment after the type changes); what the user chose is kept per target
  const [prefs, setPrefs] = useState<Record<string, { on?: boolean; note?: string }>>({});
  const keyOf = (t: Target) => `${t.field} ${t.label}`;
  const rows: Row[] = tempoTargets(type, store.fieldsOf(path)).map((t) => {
    const p = prefs[keyOf(t)] ?? {};
    return { ...t, on: p.on ?? t.on, note: p.note ?? "1/4" };
  });
  const [taps, setTaps] = useState<number[]>([]);
  const [typed, setTyped] = useState("");
  const [clamped, setClamped] = useState<string[]>([]);
  const can = useCanWrite(rows.length ? `${path}/${rows[0].field}` : undefined);
  const setlist = useSetlist();

  /** apply sets the ticked parameters to bpm; those out of their range stop at its end. */
  const apply = (bpm: number) => {
    const out: string[] = [];
    for (const r of rows) {
      if (!r.on) continue;
      const addr = `${path}/${r.field}`;
      const t = store.leaf(addr)?.field.type;
      if (!t) continue;
      const want = targetValue(r.kind, bpm, noteBeats(r.note));
      const lo = t.min ?? want;
      const hi = t.max ?? want;
      if (want < lo || want > hi) out.push(r.label);
      store.set(addr, fromDisplay(t, Math.min(hi, Math.max(lo, want))));
      store.flush(addr);
    }
    setClamped(out);
  };

  // taps live in a ref too: the T key's listener sees the last ones
  const tapsRef = useRef(taps);
  const tap = () => {
    const next = addTap(tapsRef.current, performance.now());
    tapsRef.current = next;
    setTaps(next);
    const ms = beatMs(next);
    if (ms !== null && bpmOf(ms) >= MIN_BPM && bpmOf(ms) <= MAX_BPM) apply(bpmOf(ms));
  };
  const tapRef = useRef(tap);
  tapRef.current = tap;

  // T taps, on this page (the field being typed in keeps its T)
  useEffect(() => {
    if (!can) return;
    const onKey = (e: KeyboardEvent) => {
      if (!isTapKey(e)) return;
      if ((e.target as HTMLElement).closest?.("input, textarea, select, [contenteditable]")) return;
      e.preventDefault();
      tapRef.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [can]);

  if (!rows.length) return null;

  // the tempo the console holds now, from the first ticked parameter
  const first = rows.find((r) => r.on) ?? rows[0];
  const firstAddr = `${path}/${first.field}`;
  const firstType = store.leaf(firstAddr)?.field.type;
  const firstVal = store.getValue(firstAddr);
  const now =
    firstType && typeof firstVal?.value === "number"
      ? tempoOfValue(first.kind, toDisplay(firstType, firstVal.value), noteBeats(first.note))
      : null;
  const tapped = beatMs(taps);
  const typedBpm = parseBpm(typed);

  // the song playing (the last one marked) and the next one, with their tempos
  const songs: { label: string; when: string; bpm: number }[] = [];
  if (setlist) {
    for (const [i, when] of [
      [setlist.next - 1, "Playing"],
      [setlist.next, "Next"],
    ] as const) {
      const bpm = songTempo(setlist, i);
      if (i >= 0 && i < setlist.songs.length && bpm) songs.push({ label: songLabel(i, setlist.songs[i]), when, bpm });
    }
  }

  const setRow = (i: number, change: Partial<Row>) => {
    const k = keyOf(rows[i]);
    setPrefs({ ...prefs, [k]: { ...prefs[k], ...change } });
  };

  return (
    <section className="card tap-tempo">
      <header className="section-head">
        <h3>Tempo</h3>
        {now !== null && <span className="muted">now {formatBpm(now)} BPM</span>}
      </header>
      <div className="row tap-row">
        <button
          type="button"
          className="btn primary tap-btn"
          disabled={!can}
          title="Tap the beat (or the T key): the last taps are averaged"
          // the beat is taken when the finger lands, not when it lifts
          onPointerDown={(e) => {
            if (e.button === 0) tap();
          }}
          onClick={(e) => {
            if (e.detail === 0) tap(); // Enter or Space
          }}
        >
          TAP
        </button>
        <span className="tap-readout" aria-live="polite">
          {tapped !== null ? `${formatBpm(bpmOf(tapped))} BPM` : taps.length ? "again…" : "tap the beat"}
        </span>
        <form
          className="field-inline"
          onSubmit={(e) => {
            e.preventDefault();
            if (!Number.isNaN(typedBpm)) apply(typedBpm);
          }}
        >
          <input
            className="input num"
            inputMode="decimal"
            value={typed}
            placeholder="BPM"
            aria-label="Tempo in BPM"
            onChange={(e) => setTyped(e.target.value)}
          />
          <button type="submit" className="btn" disabled={!can || Number.isNaN(typedBpm)}>
            Set
          </button>
        </form>
      </div>
      {songs.map((s) => (
        <div key={s.when} className="row tap-song">
          <span>
            {s.when}: <strong>{s.label}</strong> · {formatBpm(s.bpm)} BPM
          </span>
          <button type="button" className="btn small" disabled={!can} onClick={() => apply(s.bpm)}>
            Set {formatBpm(s.bpm)} BPM
          </button>
        </div>
      ))}
      <div className="fields tap-targets">
        {rows.map((r, i) => (
          <TargetRow key={r.field} addr={`${path}/${r.field}`} row={r} onChange={(c) => setRow(i, c)} />
        ))}
      </div>
      {clamped.length > 0 && (
        <p className="note">
          {clamped.join(", ")}: out of range for this tempo and note value, set to the nearest end. Try another note value.
        </p>
      )}
    </section>
  );
}

function TargetRow({ addr, row, onChange }: { addr: string; row: Row; onChange: (c: Partial<Row>) => void }) {
  const leaf = store.leaf(addr);
  const val = store.getValue(addr);
  const t = leaf?.field.type;
  const bpm = t && typeof val?.value === "number" ? tempoOfValue(row.kind, toDisplay(t, val.value), noteBeats(row.note)) : null;
  return (
    <div className="field">
      <label className="field-label tap-follow">
        <input type="checkbox" checked={row.on} onChange={(e) => onChange({ on: e.target.checked })} /> {row.label}
      </label>
      <span className="field-inline">
        <select
          className="select"
          value={row.note}
          aria-label={`${row.label}: note value`}
          onChange={(e) => onChange({ note: e.target.value })}
        >
          {NOTES.map((n) => (
            <option key={n.name} value={n.name}>
              {n.name}
            </option>
          ))}
        </select>
        <span className="muted">
          {t ? formatValue(t, val?.value) : ""}
          {bpm !== null && ` · ${formatBpm(bpm)} BPM`}
        </span>
      </span>
    </div>
  );
}
