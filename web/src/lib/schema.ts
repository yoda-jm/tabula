// The parameter tree from /api/schema, indexed for lookups by path and by
// parameter address.

import type { SchemaField, SchemaResponse, TypeInfo } from "./api";

export interface Field {
  name: string;
  label?: string;
  type: TypeInfo;
}

export interface Node {
  path: string;
  scene: boolean;
  /** Selector address of a variant node (effect parameters change with the effect type). */
  variant?: string;
  fields: Field[];
}

export interface Leaf {
  node: Node;
  field: Field;
  index: number;
}

export interface Schema {
  model: string;
  types: TypeInfo[];
  nodes: Node[];
  byPath: Map<string, Node>;
  leaves: Map<string, Leaf>;
  /** Top-level sections (/config, /ch, …, /-prefs) in schema order. */
  sections: string[];
}

const UNKNOWN: TypeInfo = { kind: "raw", describe: "unknown type" };

export function resolveFields(fields: SchemaField[] | null | undefined, types: TypeInfo[]): Field[] {
  return (fields ?? []).map((f) => ({
    name: f.name,
    label: f.label || undefined,
    type: f.type_info ?? types[f.type] ?? UNKNOWN,
  }));
}

export function indexSchema(res: SchemaResponse): Schema {
  const nodes: Node[] = res.nodes.map((n) => ({
    path: n.path,
    scene: n.scene,
    variant: n.variant || undefined,
    fields: resolveFields(n.fields, res.types),
  }));
  const byPath = new Map<string, Node>();
  const leaves = new Map<string, Leaf>();
  const sections: string[] = [];
  const seen = new Set<string>();
  for (const n of nodes) {
    byPath.set(n.path, n);
    n.fields.forEach((field, index) => leaves.set(n.path + "/" + field.name, { node: n, field, index }));
    const s = sectionOf(n.path);
    if (!seen.has(s)) {
      seen.add(s);
      sections.push(s);
    }
  }
  return { model: res.model, types: res.types, nodes, byPath, leaves, sections };
}

/** sectionOf returns the top-level section of a path: /ch/01/mix → /ch. */
export function sectionOf(path: string): string {
  const i = path.indexOf("/", 1);
  return i < 0 ? path : path.slice(0, i);
}

/** parentOf returns the node path of a parameter address. */
export function parentOf(addr: string): string {
  const i = addr.lastIndexOf("/");
  return i <= 0 ? addr : addr.slice(0, i);
}

export const lastSegment = (path: string) => path.slice(path.lastIndexOf("/") + 1);
