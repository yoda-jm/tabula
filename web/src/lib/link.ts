// The quality of the link to the console, measured by tabula's read-back
// (pkg/console/readback.go): the delay of a request, the requests left
// unanswered, and the changes lost on the network that it found again.

import type { Link } from "./api";

export type LinkLevel = "ok" | "warn" | "bad";

export interface LinkQuality {
  level: LinkLevel;
  /** for the top bar: "12% lost", "180 ms" */
  short: string;
  /** every figure, in words */
  detail: string;
}

const ms = (v: number) => (v < 10 ? v.toFixed(1) : String(Math.round(v))) + " ms";

/** linkQuality says how good the link is: warn from 3% unanswered or 80 ms, bad from 15% or 300 ms. */
export function linkQuality(l: Link): LinkQuality {
  const lost = l.asked ? (100 * l.lost) / l.asked : 0;
  const level: LinkLevel = lost >= 15 || l.rtt_ms >= 300 ? "bad" : lost >= 3 || l.rtt_ms >= 80 ? "warn" : "ok";
  const short = lost >= 3 ? `${Math.round(lost)}% lost` : ms(l.rtt_ms);
  const parts = [
    `${ms(l.rtt_ms)} round trip`,
    l.lost ? `${l.lost} of the last ${l.asked} requests unanswered` : `no request unanswered (of the last ${l.asked})`,
  ];
  if (l.caught) {
    const what = l.caught === 1 ? "1 change lost on the network" : `${l.caught} changes lost on the network`;
    parts.push(`${what} found again by reading the console back${l.last_caught ? ` (the last: ${l.last_caught.trim()})` : ""}`);
  }
  return { level, short, detail: parts.join(" · ") };
}
