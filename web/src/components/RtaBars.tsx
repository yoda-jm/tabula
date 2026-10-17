// The RTA bands (/meters/15, in dB) as bars, for an SVG: behind the EQ
// curve of the Channel page and behind the graphic EQ faders.

import { RTA_BANDS, rtaFraction, rtaFreq } from "../lib/rta";

const HALF_BAND = Math.pow(2, 1 / 20); // a band spans a tenth of an octave

/**
 * rtaRects draws each band from the bottom (y = height) up to its level in
 * the window, at x(frequency), clipped to [left, right].
 */
export function rtaRects(
  values: number[],
  win: { top: number; bottom: number },
  x: (f: number) => number,
  left: number,
  right: number,
  height: number,
) {
  const rects = [];
  for (let i = 0; i < Math.min(RTA_BANDS, values.length); i++) {
    const frac = rtaFraction(values[i], win);
    if (frac <= 0) continue;
    const f = rtaFreq(i);
    const x0 = Math.max(left, x(f / HALF_BAND));
    const x1 = Math.min(right, x(f * HALF_BAND));
    if (x1 > x0) rects.push(<rect key={i} x={x0} y={height * (1 - frac)} width={x1 - x0} height={height * frac} />);
  }
  return rects;
}
