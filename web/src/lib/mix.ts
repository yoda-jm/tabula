// The sources of a musician's in-ear mix: the channels, aux inputs and FX
// returns that send to their bus, in console order, pinned ones first.

import type { MixerStrip, Strip } from "./strips";
import { pairStrips } from "./strips";

export interface MixSource extends MixerStrip {
  /** Shown name: the odd strip's, or the even one's when only it has a name. */
  name: string;
  colour: number;
}

export interface SourceInfo {
  name: string;
  colour: number;
}

/**
 * mixSources lists the sources shown in an in-ear mix. A linked pair is one
 * source: the console keeps the send levels and switches of a pair in step
 * whatever the link options, so the odd strip drives both. Sources without a
 * name are left out unless showAll, or pinned.
 */
export function mixSources(
  strips: Strip[],
  linked: (flag: string) => boolean,
  info: (strip: Strip) => SourceInfo,
  opts: { showAll: boolean; pins: string[] },
): MixSource[] {
  const used = (s: string) => s !== "" && s !== "-";
  const all = pairStrips(strips, linked).map((m): MixSource => {
    const a = info(m.strip);
    const b = m.partner ? info(m.partner) : undefined;
    const name = used(a.name) || !b ? a.name : b.name;
    return { ...m, name, colour: used(a.name) || !b ? a.colour : b.colour };
  });
  const shown = all.filter((s) => opts.showAll || used(s.name) || opts.pins.includes(s.strip.path));
  const pinned = shown.filter((s) => opts.pins.includes(s.strip.path));
  return [...pinned, ...shown.filter((s) => !opts.pins.includes(s.strip.path))];
}

/**
 * masterBuses are the buses a musician's own volume (fader and mute) writes:
 * the odd bus of a stereo mix when the console follows faders and mutes in
 * linked pairs (/config/linkcfg/fdrmute ON) and mirrors it to the even one,
 * both otherwise.
 */
export function masterBuses(buses: number[], fdrmute: boolean): number[] {
  return buses.length === 2 && !fdrmute ? buses : buses.slice(0, 1);
}
