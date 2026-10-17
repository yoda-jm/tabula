// Roles and what a musician may change: the same rules as the server's
// (pkg/webapi/access.go), so controls outside a musician's mix are shown
// disabled instead of failing.

import type { Role, Whoami } from "./api";

const RANK: Record<Role, number> = { none: 0, viewer: 1, musician: 2, engineer: 3 };

export const atLeast = (r: Role | undefined, min: Role) => RANK[r ?? "none"] >= RANK[min];

const SEND = /^\/(?:ch|auxin|fxrtn)\/\d\d\/mix\/(\d\d)\/(?:on|level|pan)$/;
const BUS = /^\/bus\/(\d\d)\/mix\/(?:fader|on)$/;

/** musicianMay tells whether a musician whose mix is on buses may write addr. */
export function musicianMay(addr: string, buses: number[]): boolean {
  const m = SEND.exec(addr) ?? BUS.exec(addr);
  return !!m && buses.includes(Number(m[1]));
}

/** mayWrite tells whether this browser may write addr, writes armed or not. */
export function mayWrite(who: Whoami | null, armed: boolean, addr: string): boolean {
  if (!who) return false;
  if (who.role === "engineer") return armed;
  return who.role === "musician" && who.musicians_can_write && musicianMay(addr, who.buses ?? []);
}

export const ROLE_LABELS: Record<Role, string> = {
  none: "No access",
  viewer: "Viewer (read-only)",
  musician: "Musician (own in-ear mix)",
  engineer: "Engineer (everything)",
};
