import { describe, expect, it } from "vitest";
import type { Node } from "./schema";
import { autoOpen, buildTree, prune } from "./tree";

const node = (path: string): Node => ({ path, scene: true, fields: [] });
const NODES = ["/ch/01/config", "/ch/01/eq", "/ch/01/eq/1", "/ch/01/eq/2", "/ch/02/config", "/ch/02/eq", "/-stat", "/-stat/solosw"].map(node);

describe("buildTree", () => {
  const root = buildTree(NODES);

  it("nests the nodes by path segment, in schema order", () => {
    expect(root.children.map((c) => c.label)).toEqual(["ch", "-stat"]);
    const ch = root.children[0];
    expect(ch.node).toBeUndefined();
    expect(ch.children.map((c) => c.path)).toEqual(["/ch/01", "/ch/02"]);
    const eq = ch.children[0].children[1];
    expect(eq.path).toBe("/ch/01/eq");
    expect(eq.node?.path).toBe("/ch/01/eq");
    expect(eq.children.map((c) => c.label)).toEqual(["1", "2"]);
  });

  it("counts the nodes of each subtree", () => {
    expect(root.size).toBe(NODES.length);
    expect(root.children[0].size).toBe(6);
    expect(root.children[0].children[0].children[1].size).toBe(3);
    expect(root.children[1].size).toBe(2); // /-stat is a node with a child
  });
});

describe("prune", () => {
  const root = buildTree(NODES);

  it("keeps the matches and their parents", () => {
    const p = prune(root, (n) => n.path.includes("eq/2"))!;
    expect(p.size).toBe(1);
    const eq = p.children[0].children[0].children[0];
    expect(eq.path).toBe("/ch/01/eq");
    expect(eq.hit).toBe(false);
    expect(eq.children.map((c) => [c.path, c.hit])).toEqual([["/ch/01/eq/2", true]]);
  });

  it("returns an empty root when nothing matches", () => {
    const p = prune(root, () => false)!;
    expect(p.children).toEqual([]);
    expect(p.size).toBe(0);
  });
});

describe("autoOpen", () => {
  const root = buildTree(NODES);

  it("opens down to the matches when they fit", () => {
    const p = prune(root, (n) => n.path === "/ch/01/eq/1")!;
    expect([...autoOpen(p, 50)]).toEqual(["/ch", "/ch/01", "/ch/01/eq"]);
  });

  it("stops at the level that would list too many rows", () => {
    // ch and -stat (2 rows), then 01, 02 and solosw (5), then the 4 children of 01 and 02 (9)
    expect([...autoOpen(root, 8)].sort()).toEqual(["/-stat", "/ch"]);
    expect(autoOpen(root, 9).size).toBe(4);
    expect(autoOpen(root, 1).size).toBe(0);
  });
});
