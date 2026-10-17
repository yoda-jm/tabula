// Every node of the schema, grouped by section, filterable by path or by
// current text, each expandable into a generic editor of its fields.

import { useDeferredValue, useMemo, useState } from "react";
import { ParamControl, TextSetter } from "../components/controls";
import type { TypeInfo } from "../lib/api";
import { fieldLabel } from "../lib/labels";
import type { Node } from "../lib/schema";
import { sectionOf } from "../lib/schema";
import { useLeaf, useLine, useNode, useSchema, useStateVersion } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

const PAGE = 150;

interface Group {
  section: string;
  nodes: Node[];
  total: number;
}

export function BrowserPage() {
  const schema = useSchema();
  const version = useStateVersion();
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState<Set<string>>(new Set());
  const deferred = useDeferredValue(filter.trim().toLowerCase());

  const groups = useMemo<Group[]>(() => {
    if (!schema) return [];
    const by = new Map<string, Group>();
    for (const s of schema.sections) by.set(s, { section: s, nodes: [], total: 0 });
    for (const n of schema.nodes) {
      const g = by.get(sectionOf(n.path))!;
      g.total++;
      if (!deferred || n.path.toLowerCase().includes(deferred) || store.lineOf(n.path).toLowerCase().includes(deferred)) {
        g.nodes.push(n);
      }
    }
    return [...by.values()].filter((g) => !deferred || g.nodes.length);
  }, [schema, deferred, version]); // version: filter again on the text once the state (re)loads

  if (!schema) return <div className="page">Loading the schema…</div>;
  const toggle = (s: string) => {
    const n = new Set(open);
    if (n.has(s)) n.delete(s);
    else n.add(s);
    setOpen(n);
  };
  const matches = groups.reduce((a, g) => a + g.nodes.length, 0);
  return (
    <div className="page browser">
      <div className="browser-head">
        <input
          className="input search"
          type="search"
          placeholder="Filter by path or value: /ch/01/eq, Remy, GATE…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          aria-label="Filter nodes"
          spellCheck={false}
        />
        <span className="muted">
          {deferred ? `${matches} of ${schema.nodes.length} nodes` : `${schema.nodes.length} nodes`} · {schema.model}
        </span>
      </div>
      <NotConnected />
      {groups.map((g) => (
        <GroupView key={g.section} group={g} expanded={!!deferred || open.has(g.section)} onToggle={() => toggle(g.section)} />
      ))}
      {deferred && !matches && <p className="muted">Nothing matches “{filter}”.</p>}
    </div>
  );
}

function GroupView({ group, expanded, onToggle }: { group: Group; expanded: boolean; onToggle(): void }) {
  const [limit, setLimit] = useState(PAGE);
  const shown = group.nodes.slice(0, limit);
  return (
    <section className="group">
      <button type="button" className="group-head" aria-expanded={expanded} onClick={onToggle}>
        <span className="chev" aria-hidden>
          {expanded ? "▾" : "▸"}
        </span>
        <code>{group.section}</code>
        <span className="muted">
          {group.nodes.length === group.total ? group.total : `${group.nodes.length} / ${group.total}`} nodes
        </span>
      </button>
      {expanded && (
        <div className="group-body">
          {shown.map((n) => (
            <NodeRow key={n.path} node={n} />
          ))}
          {group.nodes.length > limit && (
            <button type="button" className="btn more" onClick={() => setLimit(limit + PAGE * 2)}>
              Show more ({group.nodes.length - limit} left)
            </button>
          )}
        </div>
      )}
    </section>
  );
}

function NodeRow({ node }: { node: Node }) {
  const [open, setOpen] = useState(false);
  const line = useLine(node.path);
  const rest = line.startsWith(node.path) ? line.slice(node.path.length) : line;
  return (
    <div className={"node" + (open ? " open" : "")}>
      <button type="button" className="node-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <code className="node-path">{node.path}</code>
        <code className="node-line">{rest}</code>
      </button>
      {open && <NodeEditor node={node} />}
    </div>
  );
}

function NodeEditor({ node }: { node: Node }) {
  const st = useNode(node.path);
  if (st?.text) {
    return (
      <div className="node-body">
        <p className="note">
          Kept as text: this effect is not modelled yet. <code className="readonly">{st.line}</code>
        </p>
      </div>
    );
  }
  const fields = store.fieldsOf(node.path);
  return (
    <div className="node-body">
      {node.variant && <p className="muted">Fields follow {node.variant} (effect type).</p>}
      {!node.scene && <p className="muted">Not part of a scene file (console status or preferences).</p>}
      {fields.map((f) => (
        <FieldEditor key={f.name} addr={`${node.path}/${f.name}`} />
      ))}
    </div>
  );
}

/** describe shortens the description of long enums. */
function describe(t: TypeInfo): string {
  if (t.kind === "enum" && (t.names?.length ?? 0) > 12) return `enum, ${t.names!.length} values (${t.names![0]} … ${t.names![t.names!.length - 1]})`;
  return t.describe;
}

function FieldEditor({ addr }: { addr: string }) {
  const leaf = useLeaf(addr);
  if (!leaf) return null;
  const { field, node } = leaf;
  const label = fieldLabel(node.path, field.name, field.label);
  return (
    <div className="field-editor">
      <div className="fe-label">
        <strong>{label}</strong>
        <code className="muted">{field.name}</code>
        <span className="muted fe-type">{describe(field.type)}</span>
      </div>
      <div className="fe-controls">
        <ParamControl addr={addr} />
        {field.type.kind !== "unused" && <TextSetter addr={addr} />}
      </div>
    </div>
  );
}
