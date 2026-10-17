// Pieces shared by the pages.

import type { ReactNode } from "react";
import { ParamControl } from "../components/controls";
import { fieldLabel } from "../lib/labels";
import { go } from "../lib/route";
import { useAccess, useLeaf, useStatus } from "../store/hooks";
import { store } from "../store/store";

/** NotConnected explains why the controls are empty. */
export function NotConnected() {
  const s = useStatus();
  const who = useAccess();
  if (s.connected) return null;
  if (s.connecting) {
    const c = s.console;
    return (
      <p className="banner">
        Syncing with the console{c?.total ? ` (${c.synced} / ${c.total})` : ""}…
      </p>
    );
  }
  if (who?.role !== "engineer") return <p className="banner">tabula is not connected to the console yet.</p>;
  return (
    <p className="banner">
      Not connected.{" "}
      <button type="button" className="linkish" onClick={() => go({ page: "connect" })}>
        Connect to a console
      </button>
    </p>
  );
}

export function Section(props: { title: string; on?: string; onLabel?: string; children?: ReactNode; className?: string }) {
  return (
    <section className={"card section " + (props.className ?? "")}>
      <header className="section-head">
        <h3>{props.title}</h3>
        {props.on && <ParamControl addr={props.on} onLabel={props.onLabel ?? "ON"} />}
      </header>
      {props.children}
    </section>
  );
}

/** Fields lays out a node's fields as label + editor rows. */
export function Fields({ path, only, skip }: { path: string; only?: string[]; skip?: string[] }) {
  const fields = store.fieldsOf(path);
  const names = fields.map((f) => f.name).filter((n) => (!only || only.includes(n)) && !skip?.includes(n));
  const order = only ? only.filter((n) => names.includes(n)) : names;
  return (
    <div className="fields">
      {order.map((n) => (
        <FieldRow key={n} addr={`${path}/${n}`} />
      ))}
    </div>
  );
}

export function FieldRow({ addr, label, mute }: { addr: string; label?: string; mute?: boolean }) {
  const leaf = useLeaf(addr);
  if (!leaf) return null;
  return (
    <div className="field">
      <span className="field-label">{label ?? fieldLabel(leaf.node.path, leaf.field.name, leaf.field.label)}</span>
      <ParamControl addr={addr} mute={mute} />
    </div>
  );
}
