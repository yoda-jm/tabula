// Hash routes: #/mixer, #/channel/ch/01, #/browser, #/record, #/connect,
// #/mix (a musician's in-ear mix; #/mix/3 shows bus 3 to the engineer),
// #/access.

import { useCallback, useEffect, useState } from "react";

export type Page = "mixer" | "channel" | "browser" | "record" | "connect" | "mix" | "access";

export interface Route {
  page: Page;
  /** the strip of the Channel page, e.g. /ch/01 */
  strip?: string;
  /** the bus of the mix page */
  bus?: number;
}

const PAGES: Page[] = ["mixer", "channel", "browser", "record", "connect", "mix", "access"];

export function parseRoute(hash: string): Route | null {
  const h = hash.replace(/^#\/?/, "");
  const [page, ...rest] = h.split("/");
  if (!PAGES.includes(page as Page)) return null;
  const r: Route = { page: page as Page };
  if (page === "channel" && rest.length) r.strip = "/" + rest.join("/");
  if (page === "mix" && /^\d+$/.test(rest[0] ?? "")) r.bus = Number(rest[0]);
  return r;
}

export function formatRoute(r: Route): string {
  if (r.page === "mix" && r.bus) return "#/mix/" + r.bus;
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

/** usePersistent is useState kept in localStorage (when the browser allows it). */
export function usePersistent<T>(key: string, initial: T): [T, (v: T) => void] {
  const [v, setV] = useState<T>(() => {
    try {
      const s = window.localStorage.getItem(key);
      return s === null ? initial : (JSON.parse(s) as T);
    } catch {
      return initial;
    }
  });
  const set = useCallback(
    (nv: T) => {
      setV(nv);
      try {
        window.localStorage.setItem(key, JSON.stringify(nv));
      } catch {
        // private window or storage disabled: keep it in memory
      }
    },
    [key],
  );
  return [v, set];
}
