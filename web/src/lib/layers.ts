// The Mixer's own layers: strips chosen by the engineer (the singers, the
// drums…) shown together, kept by the browser; and the spill of a DCA, its
// strips shown together as on the console.

import type { Strip } from "./strips";
import { ALL_STRIPS, stripByPath } from "./strips";

/** Layer is a bank of strips the engineer made. */
export interface Layer {
  id: string;
  name: string;
  /** strip paths, in the console's order */
  strips: string[];
}

/** layerStrips are a layer's strips, in the console's order. */
export const layerStrips = (l: Layer): Strip[] => ALL_STRIPS.filter((s) => l.strips.includes(s.path));

/** newLayerId gives a layer an id no other has. */
export function newLayerId(layers: Layer[]): string {
  for (let n = layers.length + 1; ; n++) if (!layers.some((l) => l.id === `l${n}`)) return `l${n}`;
}

/** SPILLABLE are the strips a DCA can control: they have a /grp/dca map. */
export const SPILLABLE = ALL_STRIPS.filter((s) => s.kind !== "dca" && s.kind !== "main");

/**
 * spillOf lists the strips of DCA n (from 1): those whose /grp/dca map has
 * its bit, then the DCA itself; dcaMap reads a strip's map.
 */
export function spillOf(n: number, dcaMap: (path: string) => number | undefined): Strip[] {
  const out = SPILLABLE.filter((s) => ((Number(dcaMap(s.path) ?? 0) >>> (n - 1)) & 1) === 1);
  const dca = stripByPath(`/dca/${n}`);
  return dca ? [...out, dca] : out;
}
