// The Server-Sent Events stream (/api/events): status, change, resync,
// record and meters. The stream is reopened with a backoff if it drops, and
// the whole state is reloaded after each (re)open, as changes may have been
// missed in between.

import type { Status } from "../lib/api";
import { api } from "../lib/api";
import { sessionApi } from "../lib/session";
import type { Store } from "./store";

/** Meter groups asked for: 0 (inputs, aux, FX returns, buses, matrices) and 2 (buses, matrices, mains). */
export const METER_GROUPS = "0,2";

export function startEvents(store: Store, meters = METER_GROUPS): () => void {
  let es: EventSource | null = null;
  let retry = 1000;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  const refreshRecord = () =>
    sessionApi.record().then(
      (r) => store.setRecord(r),
      () => store.setRecord(null), // a tabula without recording support
    );

  const open = () => {
    if (stopped) return;
    store.setStream("connecting");
    const src = new EventSource("/api/events" + (meters ? "?meters=" + meters : ""));
    es = src;
    let fresh = true; // the first status after each (re)open triggers a reload

    src.onopen = () => {
      fresh = true;
      retry = 1000;
      store.setStream("open");
    };
    src.addEventListener("status", (e) => {
      const s = JSON.parse((e as MessageEvent).data) as Status;
      store.setStatus(s);
      if (fresh) {
        fresh = false;
        if (!store.schema) void store.loadSchema(); // tabula was not reachable when the page opened
        void refreshRecord();
        if (s.connected && !s.connecting) void store.reloadState();
      }
    });
    src.addEventListener("change", (e) => {
      store.applyChanges(JSON.parse((e as MessageEvent).data));
    });
    src.addEventListener("resync", () => {
      void store.reloadState();
    });
    src.addEventListener("record", (e) => {
      store.setRecord(JSON.parse((e as MessageEvent).data));
    });
    src.addEventListener("meters", (e) => {
      const m = JSON.parse((e as MessageEvent).data) as { id: string; values: number[] };
      store.setMeters(String(m.id), m.values);
    });
    src.onerror = () => {
      store.setStream("down");
      if (src.readyState === EventSource.CLOSED) {
        src.close();
        timer = setTimeout(open, retry);
        retry = Math.min(retry * 2, 10000);
      }
      // otherwise the browser reconnects by itself
    };
  };

  // The status is also fetched once, in case the stream is slow to open.
  api.status().then(
    (s) => store.setStatus(s),
    () => store.setStream("down"),
  );
  open();

  return () => {
    stopped = true;
    if (timer) clearTimeout(timer);
    es?.close();
  };
}
