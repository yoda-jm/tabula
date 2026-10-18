// Every node of the schema as a tree of its paths (/ch → 01 → eq → 1),
// filterable by path or by current text; a node opens into a generic editor
// of its fields.

import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { ParamControl, TextSetter } from "../components/controls";
import type { TypeInfo } from "../lib/api";
import { fieldLabel } from "../lib/labels";
import { usePersistent } from "../lib/route";
import type { Node } from "../lib/schema";
import type { TreeItem } from "../lib/tree";
import { autoOpen, buildTree, prune } from "../lib/tree";
import { prettyText } from "../lib/values";
import { useLeaf, useLine, useNode, useSchema, useStateVersion, useValue } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

/** Rows a filter may list by opening the tree itself. */
const BUDGET = 200;

interface TreeCtx {
  isOpen(path: string): boolean;
  toggle(path: string): void;
  filtering: boolean;
}

export function BrowserPage() {
  const schema = useSchema();
  const version = useStateVersion();
  const [filter, setFilter] = useState("");
  const deferred = useDeferredValue(filter.trim().toLowerCase());
  // opened by hand: kept while browsing; while filtering, on top of what the filter opens
  const [opened, setOpened] = usePersistent<string[]>("tabula.browser.open", []);
  const [filterOpen, setFilterOpen] = useState<Map<string, boolean>>(new Map());
  useEffect(() => setFilterOpen(new Map()), [deferred]);

  const tree = useMemo(() => (schema ? buildTree(schema.nodes) : undefined), [schema]);
  const shown = useMemo(() => {
    if (!tree || !deferred) return tree;
    return prune(tree, (n) => n.path.toLowerCase().includes(deferred) || store.lineOf(n.path).toLowerCase().includes(deferred));
  }, [tree, deferred, version]); // version: filter again on the text once the state (re)loads
  const auto = useMemo(() => (deferred && shown ? autoOpen(shown, BUDGET) : new Set<string>()), [shown, deferred]);
  const openSet = useMemo(() => new Set(opened), [opened]);

  if (!schema || !tree || !shown) return <div className="page">Loading the schema…</div>;
  const isOpen = (p: string) => (deferred ? (filterOpen.get(p) ?? auto.has(p)) : openSet.has(p));
  const ctx: TreeCtx = {
    isOpen,
    filtering: !!deferred,
    toggle(p) {
      if (deferred) setFilterOpen(new Map(filterOpen).set(p, !isOpen(p)));
      else setOpened(openSet.has(p) ? opened.filter((x) => x !== p) : [...opened, p]);
    },
  };
  const anyOpen = deferred ? [...auto, ...filterOpen.keys()].some(isOpen) : opened.length > 0;
  const collapse = () => {
    if (deferred) setFilterOpen(new Map([...auto, ...filterOpen.keys()].map((p) => [p, false])));
    else setOpened([]);
  };
  return (
    <div className="page browser">
      <div className="browser-head">
        <input
          className="input search"
          type="search"
          placeholder="Filter by path or value: /ch/01/eq, Theo, GATE…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          aria-label="Filter nodes"
          spellCheck={false}
        />
        <span className="muted">
          {deferred ? `${shown.size} of ${tree.size} nodes` : `${tree.size} nodes`} · {schema.model}
        </span>
        <button type="button" className="btn" onClick={collapse} disabled={!anyOpen}>
          Collapse all
        </button>
      </div>
      <NotConnected />
      <TreeList items={shown.children} ctx={ctx} />
      {deferred && !shown.size && <p className="muted">Nothing matches “{filter}”.</p>}
    </div>
  );
}

function TreeList({ items, ctx }: { items: TreeItem[]; ctx: TreeCtx }) {
  return (
    <ul className="tree">
      {items.map((it) => (
        <TreeRow key={it.path} item={it} ctx={ctx} />
      ))}
    </ul>
  );
}

function TreeRow({ item, ctx }: { item: TreeItem; ctx: TreeCtx }) {
  const open = ctx.isOpen(item.path);
  const branch = item.children.length > 0;
  const unit = ctx.filtering ? (item.size === 1 ? "match" : "matches") : item.size === 1 ? "node" : "nodes";
  return (
    <li className={"tree-item" + (open ? " open" : "") + (ctx.filtering && item.node && !item.hit ? " dim" : "")}>
      <button type="button" className="tree-head" aria-expanded={open} title={item.path} onClick={() => ctx.toggle(item.path)}>
        <span className="chev" aria-hidden>
          {open ? "▾" : "▸"}
        </span>
        <code className="tree-label">{item.label}</code>
        <ItemName path={item.path} />
        {item.node && <NodeLine path={item.node.path} />}
        {branch && <span className="muted tree-size">{`${item.size} ${unit}`}</span>}
      </button>
      {open && item.node && <NodeEditor node={item.node} />}
      {open && branch && <TreeList items={item.children} ctx={ctx} />}
    </li>
  );
}

/** ItemName shows the name of a strip (/ch/01) or a DCA (/dca/1) next to its number. */
function ItemName({ path }: { path: string }) {
  const addr = path + "/config/name";
  const name = prettyText(useValue(store.leaf(addr) ? addr : undefined)?.text);
  return name ? <span className="tree-name">{name}</span> : null;
}

function NodeLine({ path }: { path: string }) {
  const line = useLine(path);
  const rest = line.startsWith(path) ? line.slice(path.length) : line;
  return <code className="tree-line">{rest}</code>;
}

function NodeEditor({ node }: { node: Node }) {
  const st = useNode(node.path);
  const path = (
    <p className="muted">
      <code>{node.path}</code>
    </p>
  );
  if (st?.text) {
    return (
      <div className="node-body">
        {path}
        <p className="note">
          Kept as text: this effect is not modelled yet. <code className="readonly">{st.line}</code>
        </p>
      </div>
    );
  }
  const fields = store.fieldsOf(node.path);
  const used = fields.filter((f) => f.type.kind !== "unused");
  return (
    <div className="node-body">
      {path}
      {node.variant && <p className="muted">Fields follow {node.variant} (effect type).</p>}
      {!node.scene && <p className="muted">Not part of a scene file (console status or preferences).</p>}
      {used.map((f) => (
        <FieldEditor key={f.name} addr={`${node.path}/${f.name}`} />
      ))}
      {used.length < fields.length && <p className="muted">{fields.length - used.length} unused positions not shown.</p>}
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
