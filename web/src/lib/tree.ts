// The schema's nodes as a tree of their path segments, for the Browser page:
// /ch → 01 → eq → 1. An item can be a node with fields (/ch/01/eq) and have
// children too (/ch/01/eq/1); others only group children (/ch, /ch/01).

import type { Node } from "./schema";

export interface TreeItem {
  /** Full path, e.g. /ch/01/eq; "" for the root. */
  path: string;
  /** Last segment of the path: eq. */
  label: string;
  /** The schema node at this path, if there is one. */
  node?: Node;
  children: TreeItem[];
  /** Schema nodes in this subtree, this one included; in a pruned tree, the matches. */
  size: number;
  /** In a pruned tree: this item's node matches. */
  hit?: boolean;
}

/** buildTree makes the tree of the nodes, children in schema order. */
export function buildTree(nodes: Node[]): TreeItem {
  const root: TreeItem = { path: "", label: "", children: [], size: 0 };
  const byPath = new Map<string, TreeItem>([["", root]]);
  for (const n of nodes) {
    let parent = root;
    for (const seg of n.path.split("/").slice(1)) {
      const path = parent.path + "/" + seg;
      let it = byPath.get(path);
      if (!it) {
        it = { path, label: seg, children: [], size: 0 };
        byPath.set(path, it);
        parent.children.push(it);
      }
      parent = it;
    }
    parent.node = n;
  }
  const count = (it: TreeItem): number => (it.size = (it.node ? 1 : 0) + it.children.reduce((a, c) => a + count(c), 0));
  count(root);
  return root;
}

/** prune keeps the items whose node matches and their parents, or null if none matches. */
export function prune(it: TreeItem, match: (n: Node) => boolean): TreeItem | null {
  const hit = !!it.node && match(it.node);
  const children = it.children.map((c) => prune(c, match)).filter((c): c is TreeItem => c !== null);
  if (!hit && !children.length && it.path !== "") return null;
  return { ...it, children, hit, size: (hit ? 1 : 0) + children.reduce((a, c) => a + c.size, 0) };
}

/**
 * autoOpen picks the items to open in a pruned tree so that it shows the
 * matches without listing more than budget rows: a whole level at a time,
 * from the top, while the next one fits.
 */
export function autoOpen(root: TreeItem, budget: number): Set<string> {
  const open = new Set<string>();
  let rows = root.children.length;
  let level = root.children.filter((c) => c.children.length);
  while (level.length) {
    const cost = level.reduce((a, c) => a + c.children.length, 0);
    if (rows + cost > budget) break;
    rows += cost;
    for (const c of level) open.add(c.path);
    level = level.flatMap((c) => c.children.filter((g) => g.children.length));
  }
  return open;
}
