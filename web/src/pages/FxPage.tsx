// Effects: an overview of the 8 FX slots (what is loaded, where it is used)
// and an editor per slot, generated from the effect's parameters as the
// server describes them, with a dedicated graphic EQ editor (GEQ, GEQ2,
// TEQ, TEQ2) showing the RTA behind the bands.

import { useMemo, useState } from "react";
import { GeqEditor } from "../components/Geq";
import { ConfirmDialog } from "../components/shell";
import { ParamControl } from "../components/controls";
import type { FxUsage, Use } from "../lib/fx";
import { fxName, fxUsage, splitDual } from "../lib/fx";
import { fieldLabel } from "../lib/labels";
import { go } from "../lib/route";
import { isGeq } from "../lib/rta";
import { useCanWrite, useLeaf, useNode, useNodes, useSchema, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";
import { NotConnected } from "./common";

const SLOTS = [1, 2, 3, 4, 5, 6, 7, 8];

/** The nodes an FX usage depends on: re-render when any changes. */
function useUsageNodes(): string {
  const schema = useSchema();
  const paths = useMemo(
    () =>
      schema
        ? schema.nodes
            .map((n) => n.path)
            .filter((p) => /^\/(fx\/\d(\/source)?|-insert|(ch|auxin|fxrtn|bus|mtx)\/\d\d\/config|main\/(st|m)\/config)$/.test(p))
        : [],
    [schema],
  );
  return useNodes(paths);
}

export function FxPage({ slot }: { slot?: number }) {
  useUsageNodes();
  return (
    <div className="page fx">
      <div className="banks" role="tablist" aria-label="Effect slots">
        <button
          type="button"
          role="tab"
          aria-selected={!slot}
          className={"bank" + (!slot ? " active" : "")}
          onClick={() => go({ page: "fx" })}
        >
          All
        </button>
        {SLOTS.map((n) => (
          <button
            key={n}
            type="button"
            role="tab"
            aria-selected={slot === n}
            className={"bank" + (slot === n ? " active" : "")}
            onClick={() => go({ page: "fx", slot: n })}
          >
            FX {n}
          </button>
        ))}
      </div>
      <NotConnected />
      {slot ? <SlotEditor slot={slot} /> : <Overview />}
    </div>
  );
}

function Overview() {
  return (
    <div className="fx-grid">
      {SLOTS.map((n) => (
        <SlotCard key={n} slot={n} usage={fxUsage(lookup, n)} />
      ))}
    </div>
  );
}

function SlotCard({ slot, usage }: { slot: number; usage: FxUsage }) {
  const unused = !usage.sources.length && !usage.inserts.length;
  return (
    <section className={"card fx-card" + (unused ? " unused" : "")}>
      <header className="section-head">
        <h3>
          FX {slot} <span className="fx-type">{usage.type || "—"}</span>
        </h3>
        <button type="button" className="btn" onClick={() => go({ page: "fx", slot })}>
          Edit
        </button>
      </header>
      <p className="fx-name">{usage.type ? fxName(usage.type) : ""}</p>
      <UsageLines usage={usage} />
    </section>
  );
}

function UsageLines({ usage }: { usage: FxUsage }) {
  const unused = !usage.sources.length && !usage.inserts.length;
  return (
    <dl className="facts fx-uses">
      {usage.sources.length > 0 && (
        <>
          <dt>Fed by</dt>
          <dd>
            <Uses uses={usage.sources} />
          </dd>
          <dt>Returns on</dt>
          <dd>
            <Uses uses={usage.returns} />
          </dd>
        </>
      )}
      {usage.inserts.length > 0 && (
        <>
          <dt>Inserted on</dt>
          <dd>
            <Uses uses={usage.inserts} />
          </dd>
        </>
      )}
      {unused && (
        <>
          <dt>Used</dt>
          <dd className="muted">nowhere (no bus feeds it, not inserted)</dd>
        </>
      )}
    </dl>
  );
}

/** Uses lists strips with links to their Channel page; the same strip on both sides is listed once. */
function Uses({ uses }: { uses: Use[] }) {
  const same = uses.length === 2 && uses[0].strip === uses[1].strip;
  const list = same ? [uses[0]] : uses;
  return (
    <span className="fx-use-list">
      {list.map((u) => (
        <a key={u.side + u.text} className="fx-use" href={u.strip ? `#/channel${u.strip}` : undefined}>
          {u.text}
          {!same && uses.length === 2 && <span className="muted"> ({u.side})</span>}
        </a>
      ))}
    </span>
  );
}

function SlotEditor({ slot }: { slot: number }) {
  const usage = fxUsage(lookup, slot);
  useNode(`/fx/${slot}/par`); // new fields when the effect changes
  return (
    <>
      <section className="card">
        <header className="section-head">
          <h3>
            FX {slot} · {usage.type ? fxName(usage.type) : "—"}
          </h3>
          <TypeSelect slot={slot} />
        </header>
        <UsageLines usage={usage} />
        {slot <= 4 && (
          <div className="fields fx-source">
            <FieldLine addr={`/fx/${slot}/source/l`} label="Source L" />
            <FieldLine addr={`/fx/${slot}/source/r`} label="Source R" />
          </div>
        )}
      </section>
      {isGeq(usage.type) ? (
        <section className="card">
          <GeqEditor slot={slot} type={usage.type} />
        </section>
      ) : (
        <Params slot={slot} />
      )}
    </>
  );
}

function FieldLine({ addr, label }: { addr: string; label: string }) {
  const leaf = useLeaf(addr);
  if (!leaf) return null;
  return (
    <div className="field">
      <span className="field-label">{label}</span>
      <ParamControl addr={addr} />
    </div>
  );
}

/** TypeSelect loads another effect after a confirmation: the slot's parameters go back to their defaults. */
function TypeSelect({ slot }: { slot: number }) {
  const addr = `/fx/${slot}/type`;
  const leaf = useLeaf(addr);
  const val = useValue(addr);
  const can = useCanWrite(addr);
  const [asking, setAsking] = useState<number | null>(null);
  const names = leaf?.field.type.names ?? [];
  return (
    <>
      <select
        className="select"
        value={val === undefined ? "" : String(val.value)}
        disabled={!can || val === undefined}
        aria-label={`FX ${slot} effect`}
        onChange={(e) => setAsking(Number(e.target.value))}
      >
        {val === undefined && <option value="">—</option>}
        {names.map((n, i) => (
          <option key={n} value={i}>
            {n} · {fxName(n)}
          </option>
        ))}
      </select>
      {asking !== null && (
        <ConfirmDialog
          title={`Load ${fxName(names[asking])} in FX ${slot}?`}
          confirm="Load"
          onCancel={() => setAsking(null)}
          onConfirm={() => {
            store.set(addr, asking);
            setAsking(null);
          }}
        >
          <p>
            The {fxName(names[Number(val?.value)] ?? "")} in FX {slot} is replaced at once, and the new effect starts from its
            default settings.
          </p>
        </ConfirmDialog>
      )}
    </>
  );
}

/** Params lays out an effect's parameters; dual effects in two columns, A and B. */
function Params({ slot }: { slot: number }) {
  const path = `/fx/${slot}/par`;
  const st = useNode(path);
  if (st?.text) {
    return (
      <section className="card">
        <p className="note">This effect is not modelled: its parameters are kept as text. {st.line}</p>
      </section>
    );
  }
  const fields = store.fieldsOf(path).filter((f) => f.type.kind !== "unused");
  if (!fields.length) return null;
  const labels = fields.map((f) => fieldLabel(path, f.name, f.label));
  const dual = splitDual(labels);
  const row = (i: number, strip = false) => (
    <div key={fields[i].name} className="field">
      <span className="field-label">{strip ? labels[i].replace(/ [AB]$/, "") : labels[i]}</span>
      <ParamControl addr={`${path}/${fields[i].name}`} />
    </div>
  );
  if (dual) {
    return (
      <div className="sections fx-dual">
        {(["a", "b"] as const).map((side) => (
          <section key={side} className="card">
            <header className="section-head">
              <h3>Side {side.toUpperCase()}</h3>
            </header>
            <div className="fields">{dual[side].map((i) => row(i, true))}</div>
          </section>
        ))}
      </div>
    );
  }
  return (
    <section className="card">
      <header className="section-head">
        <h3>Parameters</h3>
      </header>
      <div className="fields fx-params">{fields.map((_, i) => row(i))}</div>
    </section>
  );
}
