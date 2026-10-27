// Scene safes and snippet filters on the Scenes page: what every scene
// recall keeps (the show's parameter and channel safes, main levels), the
// safes of one scene, and what a snippet holds and changes (lib/safes.ts).
// Each tick writes the console's bitmap at once, armed.

import { useMemo } from "react";
import type { Bit, BitRow } from "../lib/safes";
import { CHANNEL_SAFES, MAIN_LEVELS, PARAM_SAFES, SCENE_SAFES, SHOW, isSet } from "../lib/safes";
import { sceneKeeps, sceneSafesAddr, snippetChanges, snippetRows } from "../lib/safes";
import { setBit } from "../lib/values";
import { useCanWrite, useNodes, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";

/** BitRowEditor ticks the bits of one parameter; strips show their name, and a row of strips has All and None. */
function BitRowEditor({ row, all }: { row: BitRow; all?: boolean }) {
  const val = useValue(row.addr);
  const can = useCanWrite(row.addr) && val !== undefined;
  const v = Number(val?.value ?? 0);
  const mask = row.bits.reduce((m, b) => m | (1 << b.bit), 0);
  const set = (next: number) => next !== v && store.set(row.addr, next);
  return (
    <div className="safe-row">
      <span className="field-label">{row.label}</span>
      <div className="bits named-bits" role="group" aria-label={row.label}>
        {row.bits.map((b) => (
          <BitChip key={b.bit} b={b} on={isSet(v, b.bit)} disabled={!can} onClick={() => set(setBit(v, b.bit, !isSet(v, b.bit)))} />
        ))}
        {all && (
          <>
            <button type="button" className="btn bit safe-all" disabled={!can || (v & mask) === mask} onClick={() => set(v | mask)}>
              All
            </button>
            <button type="button" className="btn bit safe-all" disabled={!can || (v & mask) === 0} onClick={() => set(v & ~mask)}>
              None
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function BitChip({ b, on, disabled, onClick }: { b: Bit; on: boolean; disabled: boolean; onClick(): void }) {
  const name = useValue(b.path ? `${b.path}/config/name` : undefined)?.value;
  const nm = typeof name === "string" ? name.trim() : "";
  return (
    <button
      type="button"
      className={"btn bit" + (on ? " lit" : "")}
      aria-pressed={on}
      disabled={disabled}
      title={nm || undefined}
      onClick={onClick}
    >
      {b.label}
      {nm && <span className="bit-name">{nm}</span>}
    </button>
  );
}

/** useStripNames re-renders when a strip of the rows is renamed. */
function useStripNames(rows: BitRow[]) {
  useNodes(useMemo(() => rows.flatMap((r) => r.bits.flatMap((b) => (b.path ? [`${b.path}/config`] : []))), [rows]));
}

/**
 * ShowSafes are the safes of every scene recall, the console's PARAM SAFE
 * and CHAN SAFE pages: what they keep, and the ticks to change it.
 */
export function ShowSafes() {
  useNodes([SHOW, "/-prefs"]);
  const keeps = sceneKeeps(lookup, 0);
  return (
    <section className="card safes">
      <h3>What every scene recall keeps</h3>
      {keeps.length ? (
        <ul className="safe-list">
          {keeps.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      ) : (
        <p className="muted">Nothing: a scene recall changes every parameter but what each scene's own safes keep (in its slot, ▾).</p>
      )}
      <details className="safes-more">
        <summary>Choose what every recall keeps</summary>
        <ShowSafesEditor />
      </details>
    </section>
  );
}

function ShowSafesEditor() {
  useStripNames(CHANNEL_SAFES);
  return (
    <div className="safes">
      <p className="muted">
        Ticked: left as it is when a scene is recalled, from tabula, the console or another editor, on top of each scene's own
        safes. A snippet changes only what it holds, whatever is ticked here.
      </p>
      <h4>Parameters</h4>
      {PARAM_SAFES.map((r) => (
        <BitRowEditor key={r.addr} row={r} />
      ))}
      <h4>Whole strips</h4>
      {CHANNEL_SAFES.map((r) => (
        <BitRowEditor key={r.addr} row={r} all />
      ))}
      <BitRowEditor row={{ addr: MAIN_LEVELS, label: "Main levels", bits: [{ bit: 0, label: "L/R and M/C faders" }] }} />
    </div>
  );
}

/** SceneSafes are the safes of one scene (scene 00 has none). */
export function SceneSafes({ slot }: { slot: number }) {
  if (slot === 0) return <p className="muted">Scene 00 has no safes of its own: what every recall keeps (below) applies.</p>;
  return (
    <div className="safes">
      <BitRowEditor row={{ addr: sceneSafesAddr(slot), label: "This scene's recall keeps", bits: SCENE_SAFES }} />
    </div>
  );
}

/** SnippetHolds is what a snippet about to be saved will hold, the ticks folded under it. */
export function SnippetHolds({ slot }: { slot: number }) {
  useNodes([`/-show/showfile/snippet/${String(slot).padStart(3, "0")}`]);
  const holds = snippetChanges(lookup, slot);
  return (
    <details className="safes-more">
      <summary>Holds {holds || "nothing yet: tick what it should hold"}</summary>
      <SnippetFilters slot={slot} />
    </details>
  );
}

/** SnippetFilters are what a snippet holds and changes. */
export function SnippetFilters({ slot }: { slot: number }) {
  const { params, strips } = useMemo(() => snippetRows(slot), [slot]);
  useStripNames(strips);
  return (
    <div className="safes">
      <p className="muted">
        What this snippet holds and changes: the ticked parameters of the ticked strips, the ticked effects and console
        settings. It holds what was ticked when it was saved: tick before saving.
      </p>
      {params.map((r) => (
        <BitRowEditor key={r.label} row={r} />
      ))}
      {strips.map((r) => (
        <BitRowEditor key={r.addr} row={r} all />
      ))}
    </div>
  );
}
