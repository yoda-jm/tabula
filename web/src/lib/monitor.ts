// Solo, mute groups, talkback and the oscillator: where the console keeps
// them. Solo switches are /-stat/solosw/01-80 in the strip order of
// ALL_STRIPS (channels, aux ins, FX returns, buses, matrices, LR, M/C,
// DCAs); /-stat/solo is ON while any is. A strip in a mute group that is on,
// or in a DCA that is muted, is silent though its own mute is off.

import { range, two } from "./num";
import type { Strip } from "./strips";
import { ALL_STRIPS } from "./strips";

/** soloAddr is a strip's solo switch. */
export function soloAddr(strip: Strip): string | undefined {
  const i = ALL_STRIPS.findIndex((s) => s.path === strip.path);
  return i < 0 ? undefined : `/-stat/solosw/${two(i + 1)}`;
}

/** SOLO_ADDRS are the 80 solo switches, with their strips. */
export const SOLO_ADDRS = ALL_STRIPS.map((strip, i) => ({ strip, addr: `/-stat/solosw/${two(i + 1)}` }));

/** MUTE_GROUPS are the console's six mute groups. */
export const MUTE_GROUPS = range(1, 6);

export const muteGroupAddr = (n: number) => `/config/mute/${n}`;

/** Talkback destinations, bit by bit of /config/talk/A|B/destmap. */
export const TALK_DESTS = [...range(1, 16).map((n) => `Bus ${two(n)}`), "LR", "M/C"];

/** Oscillator destinations, as /config/osc/dest numbers them. */
export const OSC_DESTS = [...range(1, 16).map((n) => `Bus ${two(n)}`), "L", "R", "L+R", "M/C", ...range(1, 6).map((n) => `Mtx ${n}`)];

/** groupsOf lists the groups (from 1) set in a /grp/mute or /grp/dca bitmap. */
export function groupsOf(bitmap: number, count: number): number[] {
  return range(1, count).filter((n) => ((bitmap >>> (n - 1)) & 1) === 1);
}

/**
 * silencedBy says why a strip is silent though its own mute is off: the mute
 * groups that are on, and the muted DCAs, it belongs to.
 */
export function silencedBy(grp: { mute: number; dca: number }, groupOn: (n: number) => boolean, dcaOn: (n: number) => boolean): string[] {
  return [
    ...groupsOf(grp.mute, 6)
      .filter(groupOn)
      .map((n) => `mute group ${n}`),
    ...groupsOf(grp.dca, 8)
      .filter((n) => !dcaOn(n))
      .map((n) => `DCA ${n}`),
  ];
}
