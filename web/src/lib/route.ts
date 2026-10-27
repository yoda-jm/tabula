// Hash routes: #/mixer, #/channel/ch/01, #/meters, #/monitor, #/routing, #/fx (#/fx/5:
// FX slot 5), #/scenes, #/usb, #/browser, #/check, #/record, #/connect, #/mix
// (a musician's in-ear mix; #/mix/3 shows bus 3 to the engineer), #/access.

import { useCallback, useEffect, useState } from "react";

export type Page =
  | "mixer"
  | "channel"
  | "meters"
  | "monitor"
  | "routing"
  | "fx"
  | "scenes"
  | "usb"
  | "browser"
  | "check"
  | "record"
  | "connect"
  | "mix"
  | "access";

export interface Route {
  page: Page;
  /** the strip of the Channel page, e.g. /ch/01 */
  strip?: string;
  /** the bus of the mix page */
  bus?: number;
  /** the effect slot of the FX page, 1-8 */
  slot?: number;
}

const PAGES: Page[] = ["mixer", "channel", "meters", "monitor", "routing", "fx", "scenes", "usb", "browser", "check", "record", "connect", "mix", "access"];

export function parseRoute(hash: string): Route | null {
  const h = hash.replace(/^#\/?/, "");
  const [page, ...rest] = h.split("/");
  if (!PAGES.includes(page as Page)) return null;
  const r: Route = { page: page as Page };
  if (page === "channel" && rest.length) r.strip = "/" + rest.join("/");
  if (page === "mix" && /^\d+$/.test(rest[0] ?? "")) r.bus = Number(rest[0]);
  if (page === "fx" && /^[1-8]$/.test(rest[0] ?? "")) r.slot = Number(rest[0]);
  return r;
}

export function formatRoute(r: Route): string {
  if (r.page === "mix" && r.bus) return "#/mix/" + r.bus;
  if (r.page === "fx" && r.slot) return "#/fx/" + r.slot;
  return "#/" + r.page + (r.page === "channel" && r.strip ? r.strip : "");
}

export function useHashRoute(): Route | null {
  const [route, setRoute] = useState(() => parseRoute(window.location.hash));
  useEffect(() => {
    const on = () => setRoute(parseRoute(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return route;
}

export function go(r: Route) {
  const h = formatRoute(r);
  if (window.location.hash !== h) window.location.hash = h;
}

/** readPersistent reads a value kept by usePersistent, or initial. */
export function readPersistent<T>(key: string, initial: T): T {
  try {
    const s = window.localStorage.getItem(key);
    return s === null ? initial : (JSON.parse(s) as T);
  } catch {
    return initial;
  }
}

/** followKey is usePersistent's state for key: the one held, or the value kept under a new key (pins per bus). */
export function followKey<T>(held: { key: string; v: T }, key: string, initial: T): { key: string; v: T } {
  return held.key === key ? held : { key, v: readPersistent(key, initial) };
}

/** usePersistent is useState kept in localStorage (when the browser allows it), under key: a new key reads its own value. */
export function usePersistent<T>(key: string, initial: T): [T, (v: T) => void] {
  const [held, setHeld] = useState(() => ({ key, v: readPersistent(key, initial) }));
  const cur = followKey(held, key, initial);
  if (cur !== held) setHeld(cur); // during the render: the old key's value never shows under the new one
  const set = useCallback(
    (nv: T) => {
      setHeld({ key, v: nv });
      try {
        window.localStorage.setItem(key, JSON.stringify(nv));
      } catch {
        // private window or storage disabled: keep it in memory
      }
    },
    [key],
  );
  return [cur.v, set];
}
