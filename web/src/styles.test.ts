// A rule left open in styles.css nests the rules after it (CSS nesting):
// they then match nothing, and a whole page loses its layout silently.

import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("styles.css", () => {
  it("closes every rule", () => {
    const css = readFileSync(new URL("./styles.css", import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
    const open: string[] = [];
    const nested: string[] = [];
    let sel = "";
    for (const ch of css) {
      if (ch === "{") {
        const parent = open[open.length - 1];
        if (parent !== undefined && !parent.startsWith("@")) nested.push(`${sel.trim()} (in ${parent})`);
        open.push(sel.trim());
        sel = "";
      } else if (ch === "}") {
        open.pop();
        sel = "";
      } else if (ch === ";") sel = "";
      else sel += ch;
    }
    expect(nested).toEqual([]);
    expect(open).toEqual([]);
  });
});
