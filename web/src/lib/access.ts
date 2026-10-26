// Roles and what a musician may change: the same rules as the server's
// (pkg/webapi/access.go), so controls outside a musician's mix are shown
// disabled instead of failing.

import type { RawValue, Role, Whoami } from "./api";

const RANK: Record<Role, number> = { none: 0, viewer: 1, musician: 2, engineer: 3 };

export const atLeast = (r: Role | undefined, min: Role) => RANK[r ?? "none"] >= RANK[min];

const SEND = /^\/(?:ch|auxin|fxrtn)\/\d\d\/mix\/(\d\d)\/(?:on|level|pan)$/;
const BUS = /^\/bus\/(\d\d)\/mix\/(?:fader|on)$/;

/** musicianMay tells whether a musician whose mix is on buses may write addr. */
export function musicianMay(addr: string, buses: number[]): boolean {
  const m = SEND.exec(addr) ?? BUS.exec(addr);
  return !!m && buses.includes(Number(m[1]));
}

/** busLinkAddr is the link switch of the pair of buses a bus belongs to: /config/buslink/3-4 for bus 3 or 4. */
export function busLinkAddr(bus: number): string {
  const odd = bus - ((bus + 1) % 2);
  return `/config/buslink/${odd}-${odd + 1}`;
}

/** mixBuses is the mix of a musician on bus: the bus, or the pair it belongs to when linked (stereo), as the server's mixBuses. */
export function mixBuses(bus: number, linked: boolean): number[] {
  if (bus < 1 || bus > 16) return [];
  const odd = bus - ((bus + 1) % 2);
  return linked ? [odd, odd + 1] : [bus];
}

/**
 * musicianBuses follows the link switch of the musician's bus, read with
 * value, so that a pair linked later is theirs at once; until the switch is
 * known, the buses /api/whoami gave.
 */
export function musicianBuses(who: Whoami, value?: (addr: string) => RawValue | undefined): number[] {
  if (!who.bus) return who.buses ?? [];
  const link = value?.(busLinkAddr(who.bus));
  return link === undefined ? (who.buses ?? [who.bus]) : mixBuses(who.bus, Number(link) === 1);
}

/** mayWrite tells whether this browser may write addr, writes armed or not; value reads the console's values. */
export function mayWrite(who: Whoami | null, armed: boolean, addr: string, value?: (addr: string) => RawValue | undefined): boolean {
  if (!who) return false;
  if (who.role === "engineer") return armed;
  return who.role === "musician" && who.musicians_can_write && musicianMay(addr, musicianBuses(who, value));
}

export const ROLE_LABELS: Record<Role, string> = {
  none: "No access",
  viewer: "Viewer (read-only)",
  musician: "Musician (own in-ear mix)",
  engineer: "Engineer (everything)",
};
