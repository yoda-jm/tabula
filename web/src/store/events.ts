// The Server-Sent Events stream (/api/events): status, change, resync,
// record, access and meters. The stream is reopened with a backoff if it
// drops, and the whole state is reloaded after each (re)open, as changes may
// have been missed in between. A stream that drops because the access was
// deleted is not reopened.
//
// The meter groups are named when the stream opens. When a view wants
// another group (store.wantMeters, e.g. the RTA), a second stream is opened
// with the new list and the first one is closed once the second has
// started: no change is missed in between, so the state is not reloaded.

import type { Status } from "../lib/api";
import { api } from "../lib/api";
import { sessionApi } from "../lib/session";
import type { Store } from "./store";

/** Meter groups asked for: 0 (inputs, aux, FX returns, buses, matrices) and 2 (buses, matrices, mains). */
export const METER_GROUPS = "0,2";

/** SWAP_DELAY lets a burst of wantMeters calls (a page opening) settle into one new stream. */
export const SWAP_DELAY = 150;

export function startEvents(store: Store, meters = METER_GROUPS): () => void {
  let es: EventSource | null = null; // the stream in use
  let next: EventSource | null = null; // a stream about to replace it (other meter groups)
  let groups = ""; // the meter groups of es
  let nextGroups = ""; // and of next
  let retry = 1000;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let swapTimer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  const wantedList = () => [...new Set([...meters.split(","), ...store.meterSet()].filter(Boolean))];
  const wanted = () => wantedList().join(",");

  const refreshRecord = () =>
    sessionApi.record().then(
      (r) => store.setRecord(r),
      () => store.setRecord(null), // a tabula without recording support
    );

  /** open opens a stream; with swap, it replaces the open one once it has started. */
  const open = (swap = false) => {
    if (stopped) return;
    const g = wanted();
    if (swap) {
      next?.close();
      nextGroups = g;
    } else {
      // a fresh start replaces both streams
      es?.close();
      next?.close();
      next = null;
      groups = g;
      store.setStream("connecting");
    }
    const src = new EventSource("/api/events" + (g ? "?meters=" + g : ""));
    if (swap) next = src;
    else es = src;
    const live = () => src === es || src === next;
    let fresh = true; // the first status after each (re)open triggers a reload

    src.onopen = () => {
      if (src !== es) return;
      fresh = true;
      retry = 1000;
      store.setStream("open");
    };
    src.addEventListener("status", (e) => {
      if (!live()) return;
      const s = JSON.parse((e as MessageEvent).data) as Status;
      store.setStatus(s);
      if (src === next) {
        // Every change from now on comes on this stream too: the old one can go.
        const wasOpen = store.stream === "open";
        es?.close();
        es = src;
        groups = nextGroups;
        next = null;
        retry = 1000;
        if (timer) {
          clearTimeout(timer); // the old stream's reopen is not needed any more
          timer = null;
        }
        store.setStream("open");
        if (wasOpen) {
          fresh = false;
          return;
        }
      }
      if (fresh) {
        fresh = false;
        if (!store.schema) void store.loadSchema(); // tabula was not reachable when the page opened
        void refreshRecord();
        if (s.connected && !s.connecting) void store.reloadState();
      }
    });
    src.addEventListener("change", (e) => {
      if (live()) store.applyChanges(JSON.parse((e as MessageEvent).data));
    });
    src.addEventListener("resync", () => {
      if (live()) void store.reloadState();
    });
    src.addEventListener("record", (e) => {
      if (live()) store.setRecord(JSON.parse((e as MessageEvent).data));
    });
    src.addEventListener("access", () => {
      if (live()) void store.loadAccess();
    });
    src.addEventListener("meters", (e) => {
      if (!live()) return;
      const m = JSON.parse((e as MessageEvent).data) as { id: string; values: number[] };
      // a group given back keeps coming until the stream is replaced: drop it
      if (wantedList().includes(String(m.id))) store.setMeters(String(m.id), m.values);
    });
    src.onerror = () => {
      if (src === next) {
        // the replacement failed: keep the stream in use, try again later
        src.close();
        next = null;
        scheduleSwap(retry);
        return;
      }
      if (src !== es) return;
      store.setStream("down");
      if (src.readyState === EventSource.CLOSED) {
        src.close();
        void store.loadAccess().then((who) => {
          if (who?.role === "none") stopped = true;
        });
        timer = setTimeout(() => {
          timer = null;
          open();
        }, retry);
        retry = Math.min(retry * 2, 10000);
      }
      // otherwise the browser reconnects by itself
    };
  };

  /** scheduleSwap reopens the stream if the wanted meter groups changed. */
  const scheduleSwap = (delay = SWAP_DELAY) => {
    if (swapTimer) clearTimeout(swapTimer);
    swapTimer = setTimeout(() => {
      swapTimer = null;
      const want = wanted();
      if (stopped || want === (next ? nextGroups : groups)) return;
      if (!es || es.readyState === EventSource.CLOSED) return; // the next reopen asks for them
      open(true);
    }, delay);
  };
  const unsubscribe = store.subscribe("meterset", () => scheduleSwap());

  // The status is also fetched once, in case the stream is slow to open.
  api.status().then(
    (s) => store.setStatus(s),
    () => store.setStream("down"),
  );
  open();

  return () => {
    stopped = true;
    unsubscribe();
    if (timer) clearTimeout(timer);
    if (swapTimer) clearTimeout(swapTimer);
    es?.close();
    next?.close();
  };
}
