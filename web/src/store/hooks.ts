// React bindings of the store.

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { busLinkAddr } from "../lib/access";
import type { Leaf } from "../lib/schema";
import { parentOf } from "../lib/schema";
import { store } from "./store";
import type { NodeState, Val } from "./store";

function useKey<T>(key: string, get: () => T): T {
  const sub = useCallback((fn: () => void) => store.subscribe(key, fn), [key]);
  return useSyncExternalStore(sub, get, get);
}

export const useStatus = () => useKey("status", () => store.status);
export const useStream = () => useKey("stream", () => store.stream);
export const useRecord = () => useKey("record", () => store.record);
export const useSceneLoad = () => useKey("scenefile", () => store.sceneLoad);
export const useOnline = () => useKey("online", () => store.online);
export const useToasts = () => useKey("toasts", () => store.toasts);
export const useSchema = () => useKey("schema", () => store.schema); // loads notify every key
export const useStateVersion = () => useKey("state", () => store.stateVersion);

export function useValue(addr: string | undefined): Val | undefined {
  const key = addr ? "v:" + addr : "";
  return useKey(
    key,
    useCallback(() => (addr ? store.getValue(addr) : undefined), [addr]),
  );
}

export function useNode(path: string): NodeState | undefined {
  return useKey(
    "n:" + path,
    useCallback(() => store.getNode(path), [path]),
  );
}

/** useLine returns the current node text of a node. */
export function useLine(path: string): string {
  return useKey(
    "n:" + path,
    useCallback(() => store.lineOf(path), [path]),
  );
}

/** useLeaf returns the field and type of a parameter (variant aware). */
export function useLeaf(addr: string | undefined): Leaf | undefined {
  const path = addr ? parentOf(addr) : "";
  useNode(path); // re-render when a variant node gets new fields
  useSchema();
  return addr ? store.leaf(addr) : undefined;
}

export const useAccess = () => useKey("access", () => store.access);

/**
 * useCanWrite is true when controls are enabled: connected, and for the
 * engineer armed; for addr, also when a musician's access covers it.
 */
export function useCanWrite(addr?: string): boolean {
  useStatus();
  const who = useAccess();
  useValue(who?.role === "musician" && who.bus ? busLinkAddr(who.bus) : undefined); // a musician's mix follows its bus link
  return store.canWrite(addr);
}

/** useMeters returns a meter group's latest values (re-renders ~20 times per second). */
export function useMeters(id: string): number[] | undefined {
  return useKey(
    "m:" + id,
    useCallback(() => store.getMeters(id), [id]),
  );
}

/**
 * useMeterGroup subscribes to a meter group outside the default ones (e.g.
 * "15", the RTA) while the component is shown, or not at all for null.
 */
export function useMeterGroup(id: string | null): number[] | undefined {
  useEffect(() => (id ? store.wantMeters(id) : undefined), [id]);
  return useMeters(id ?? "");
}

/** useNodes re-renders when any of the nodes changes (one subscription per node). */
export function useNodes(paths: string[]): string {
  const key = paths.join("|");
  const sub = useCallback(
    (fn: () => void) => {
      const offs = key.split("|").map((p) => store.subscribe("n:" + p, fn));
      return () => offs.forEach((off) => off());
    },
    [key],
  );
  const get = useCallback(
    () =>
      key
        .split("|")
        .map((p) => store.lineOf(p))
        .join("\n"),
    [key],
  );
  return useSyncExternalStore(sub, get, get);
}
