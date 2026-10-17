// The client store: the schema, a mirror of the console values keyed by
// parameter address, the connection status, and the writes in flight.
//
// Components subscribe to keys ("v:/ch/01/mix/fader", "n:/ch/01/mix",
// "status"…) through useSyncExternalStore (see hooks.ts), so a change
// re-renders only what shows it.
//
// Writes are optimistic: the control shows its new value at once, and
// echoes of older writes (the event stream lags behind a fast fader drag)
// are held back until the console confirms the last value sent, or a hold
// time passes.

import { ApiError, api as realApi } from "../lib/api";
import type { Change, RawValue, StateNode, Status } from "../lib/api";
import { indexSchema, parentOf, resolveFields } from "../lib/schema";
import type { Field, Leaf, Schema } from "../lib/schema";
import { KeyedThrottle } from "../lib/throttle";
import { formatValue, isFloatKind, quantize, sameValue } from "../lib/values";
import type { Record as RecordStatus } from "../lib/session";

export interface Val {
  value: RawValue;
  text: string;
  /** true while a write of ours is not confirmed yet */
  pending?: boolean;
}

export interface NodeState {
  line: string;
  /** kept as text by the server (not modelled yet): only line is meaningful */
  text?: boolean;
  /** current fields of a variant node (effect parameters) */
  fields?: Field[];
}

interface Pending {
  value: RawValue;
  until: number;
  prev?: Val;
  latest?: Val; // the last value the server reported while we held it back
}

export interface Toast {
  id: number;
  text: string;
  kind: "info" | "warn" | "error";
}

export type StreamState = "connecting" | "open" | "down";

export type Api = typeof realApi;

export const WRITE_INTERVAL = 33; // ms between two writes of one continuous control (~30/s)
export const HOLD = 1500; // ms a local value wins over stale echoes

export class Store {
  schema: Schema | null = null;
  schemaError = "";
  status: Status = { connected: false, connecting: false };
  stream: StreamState = "connecting";
  record: RecordStatus | null = null;
  private recordAt = 0;
  toasts: Toast[] = [];
  /** bumped when the whole state is (re)loaded or cleared */
  stateVersion = 0;
  stateLoaded = false;

  private values = new Map<string, Val>();
  private nodes = new Map<string, NodeState>();
  private pending = new Map<string, Pending>();
  private listeners = new Map<string, Set<() => void>>();
  private throttle: KeyedThrottle<RawValue>;
  private loading = false;
  private reloadAgain = false;
  private buffered: Change[] = [];
  private refreshTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private sweepTimer: ReturnType<typeof setTimeout> | null = null;
  private toastId = 0;
  private meterValues = new Map<string, number[]>();

  constructor(
    private api: Api = realApi,
    private now: () => number = Date.now,
  ) {
    this.throttle = new KeyedThrottle((addr, v) => this.send(addr, v), WRITE_INTERVAL);
  }

  // --- subscriptions ---

  subscribe = (key: string, fn: () => void): (() => void) => {
    let set = this.listeners.get(key);
    if (!set) {
      set = new Set();
      this.listeners.set(key, set);
    }
    set.add(fn);
    return () => {
      set!.delete(fn);
      if (set!.size === 0) this.listeners.delete(key);
    };
  };

  private notify(keys: Iterable<string>) {
    for (const k of keys) {
      const set = this.listeners.get(k);
      if (set) for (const fn of [...set]) fn();
    }
  }

  private notifyAll() {
    this.notify([...this.listeners.keys()]);
  }

  // --- reads ---

  getValue = (addr: string): Val | undefined => this.values.get(addr);
  getNode = (path: string): NodeState | undefined => this.nodes.get(path);

  /** leaf finds a parameter's field and type, using the current fields of variant nodes. */
  leaf(addr: string): Leaf | undefined {
    if (!this.schema) return undefined;
    const direct = this.schema.leaves.get(addr);
    if (direct && !direct.node.variant) return direct;
    const path = parentOf(addr);
    const node = this.schema.byPath.get(path);
    if (!node) return undefined;
    const fields = this.fieldsOf(path);
    const name = addr.slice(path.length + 1);
    const index = fields.findIndex((f) => f.name === name);
    return index < 0 ? undefined : { node, field: fields[index], index };
  }

  /** fieldsOf returns the fields of a node: its current ones if it is a variant node. */
  fieldsOf(path: string): Field[] {
    const node = this.schema?.byPath.get(path);
    if (!node) return [];
    if (node.variant) {
      const st = this.nodes.get(path);
      if (st?.text) return [];
      return st?.fields ?? node.fields;
    }
    return node.fields;
  }

  /** lineOf composes a node's text from its current values, as the console writes it. */
  lineOf(path: string): string {
    const st = this.nodes.get(path);
    if (st?.text) return st.line;
    const fields = this.fieldsOf(path);
    if (!fields.length) return st?.line ?? path;
    const texts: string[] = [];
    for (const f of fields) {
      const v = this.values.get(path + "/" + f.name);
      if (!v) return st?.line ?? path;
      texts.push(v.text);
    }
    return path + " " + texts.join(" ");
  }

  canWrite(): boolean {
    return !!(this.status.connected && this.status.console?.armed);
  }

  // --- schema and state loading ---

  async loadSchema() {
    try {
      this.schema = indexSchema(await this.api.schema());
      this.schemaError = "";
    } catch (e) {
      this.schemaError = (e as Error).message;
    }
    this.notifyAll();
  }

  /** reloadState fetches every value (after connecting, on resync, after the stream dropped). */
  async reloadState() {
    if (this.loading) {
      this.reloadAgain = true;
      return;
    }
    this.loading = true;
    this.buffered = [];
    try {
      const nodes = await this.api.state();
      this.values.clear();
      this.nodes.clear();
      this.loadNodes(nodes);
      this.stateLoaded = true;
      this.stateVersion++;
      const buffered = this.buffered;
      this.buffered = [];
      this.loading = false;
      if (buffered.length) this.applyChanges(buffered, false);
      this.notifyAll();
    } catch (e) {
      this.loading = false;
      if (!(e instanceof ApiError && e.status === 409)) this.toast(`Could not load the console state: ${(e as Error).message}`, "error");
    }
    if (this.reloadAgain) {
      this.reloadAgain = false;
      void this.reloadState();
    }
  }

  /** refresh reloads the nodes under a prefix (variant nodes after their selector changed). */
  async refresh(prefix: string) {
    try {
      const nodes = await this.api.state(prefix);
      const keys = this.loadNodes(nodes);
      this.notify(keys);
    } catch {
      // not connected any more: the next resync reloads everything
    }
  }

  private scheduleRefresh(prefix: string, delay = 250) {
    const old = this.refreshTimers.get(prefix);
    if (old) clearTimeout(old);
    this.refreshTimers.set(
      prefix,
      setTimeout(() => {
        this.refreshTimers.delete(prefix);
        void this.refresh(prefix);
      }, delay),
    );
  }

  private loadNodes(nodes: StateNode[]): Set<string> {
    const keys = new Set<string>();
    const types = this.schema?.types ?? [];
    for (const n of nodes) {
      const st: NodeState = { line: n.line };
      if (n.text) st.text = true;
      if (n.fields) st.fields = resolveFields(n.fields, types);
      this.nodes.set(n.path, st);
      keys.add("n:" + n.path);
      for (const v of n.values ?? []) {
        if (this.pending.has(v.addr)) {
          this.pending.get(v.addr)!.latest = { value: v.value, text: v.text };
          continue;
        }
        this.values.set(v.addr, { value: v.value, text: v.text });
        keys.add("v:" + v.addr);
      }
    }
    return keys;
  }

  clearState() {
    this.values.clear();
    this.nodes.clear();
    this.pending.clear();
    this.throttle.cancelAll();
    this.stateLoaded = false;
    this.stateVersion++;
    this.notifyAll();
  }

  // --- events ---

  setStatus(s: Status) {
    const prev = this.status;
    this.status = s;
    if ((s.connecting && !prev.connecting) || (!s.console && !s.connecting && this.stateLoaded)) {
      this.clearState();
    }
    this.notify(["status"]);
  }

  setStream(s: StreamState) {
    if (this.stream === s) return;
    this.stream = s;
    this.notify(["stream"]);
  }

  setRecord(r: RecordStatus | null) {
    this.record = r;
    this.recordAt = this.now();
    this.notify(["record"]);
  }

  /** recordElapsed is the recording's elapsed time: the server's duration plus the time since it was sent. */
  recordElapsed(): number {
    const r = this.record;
    if (!r?.recording) return 0;
    return (r.meta?.duration ?? 0) + (this.now() - this.recordAt) / 1000;
  }

  /** applyChanges applies a "change" event: an array of parameter changes. */
  applyChanges(changes: Change[], notify = true): Set<string> {
    if (this.loading) {
      this.buffered.push(...changes);
      return new Set();
    }
    const keys = new Set<string>();
    const now = this.now();
    for (const c of changes) {
      const path = parentOf(c.addr);
      if (/^\/fx\/\d+\/type$/.test(c.addr)) this.scheduleRefresh(c.addr.replace(/type$/, "par"));
      const st = this.nodes.get(path);
      if (st?.text || (this.schema && !this.leaf(c.addr))) {
        // a node kept as text: the change cannot be applied field by field
        this.scheduleRefresh(path);
        continue;
      }
      const val: Val = { value: c.value, text: c.text };
      const p = this.pending.get(c.addr);
      if (p && now < p.until) {
        const t = this.leaf(c.addr)?.field.type;
        if (sameValue(t, c.value, p.value)) {
          this.pending.delete(c.addr);
        } else {
          p.latest = val; // an echo of an older write, or a change made elsewhere while we drag
          continue;
        }
      } else if (p) {
        this.pending.delete(c.addr);
      }
      this.values.set(c.addr, val);
      keys.add("v:" + c.addr);
      keys.add("n:" + path);
    }
    if (notify) this.notify(keys);
    return keys;
  }

  // --- meters ---

  setMeters(id: string, values: number[]) {
    this.meterValues.set(id, values);
    this.notify(["m:" + id]);
  }

  getMeters = (id: string): number[] | undefined => this.meterValues.get(id);

  // --- writes ---

  /**
   * set writes a raw value (normalized float, enum index, int, string).
   * Floats are throttled per address; every view of the address shows the
   * new value at once.
   */
  set(addr: string, value: RawValue) {
    if (!this.canWrite()) {
      this.toast(this.status.connected ? "Writes are not armed." : "Not connected.", "warn");
      return;
    }
    const leaf = this.leaf(addr);
    if (!leaf) return;
    const t = leaf.field.type;
    const float = isFloatKind(t);
    const v: RawValue = float && typeof value === "number" ? quantize(value, t.steps) : value;
    const cur = this.values.get(addr);
    const p = this.pending.get(addr);
    this.pending.set(addr, { value: v, until: this.now() + HOLD, prev: p ? p.prev : cur, latest: p?.latest });
    const text = t.kind === "string" ? `"${v}"` : formatValue(t, v);
    this.values.set(addr, { value: v, text, pending: true });
    this.notify(["v:" + addr, "n:" + leaf.node.path]);
    if (float) this.throttle.push(addr, v);
    else this.send(addr, v);
    this.scheduleSweep();
  }

  /** flush sends the last throttled value of addr now (end of a drag). */
  flush(addr: string) {
    this.throttle.flush(addr);
  }

  /** setText writes node text as typed on the console ("-6", "GATE", "1k20"). */
  async setText(addr: string, text: string): Promise<boolean> {
    if (!this.canWrite()) {
      this.toast("Writes are not armed.", "warn");
      return false;
    }
    try {
      await this.api.set({ addr, text });
      return true;
    } catch (e) {
      this.toast(`${addr}: ${(e as Error).message}`, "error");
      return false;
    }
  }

  private send(addr: string, value: RawValue) {
    const p = this.pending.get(addr);
    if (p) p.until = this.now() + HOLD;
    this.api.set({ addr, value }).catch((e: Error) => {
      this.revert(addr);
      this.toast(`${addr}: ${e.message}`, "error");
    });
  }

  private revert(addr: string) {
    const p = this.pending.get(addr);
    if (!p) return;
    this.pending.delete(addr);
    const back = p.latest ?? p.prev;
    if (back) this.values.set(addr, back);
    else this.values.delete(addr);
    const leaf = this.leaf(addr);
    this.notify(["v:" + addr, ...(leaf ? ["n:" + leaf.node.path] : [])]);
  }

  /** sweep settles the writes whose hold time passed without a confirmation. */
  sweep() {
    const now = this.now();
    const keys: string[] = [];
    const unconfirmed = new Set<string>();
    for (const [addr, p] of this.pending) {
      if (now < p.until) continue;
      this.pending.delete(addr);
      const path = parentOf(addr);
      if (p.latest) {
        this.values.set(addr, p.latest);
      } else {
        const v = this.values.get(addr);
        if (v) this.values.set(addr, { value: v.value, text: v.text });
        unconfirmed.add(path); // no answer: ask the server what it holds
      }
      keys.push("v:" + addr, "n:" + path);
    }
    this.notify(keys);
    for (const path of unconfirmed) this.scheduleRefresh(path, 0);
    if (this.pending.size) this.scheduleSweep();
  }

  private scheduleSweep() {
    if (this.sweepTimer) return;
    this.sweepTimer = setTimeout(() => {
      this.sweepTimer = null;
      this.sweep();
    }, 250);
  }

  // --- toasts ---

  toast(text: string, kind: Toast["kind"] = "info") {
    const last = this.toasts[this.toasts.length - 1];
    if (last && last.text === text) return;
    const id = ++this.toastId;
    this.toasts = [...this.toasts.slice(-3), { id, text, kind }];
    this.notify(["toasts"]);
    setTimeout(() => this.dismiss(id), kind === "error" ? 8000 : 4000);
  }

  dismiss(id: number) {
    this.toasts = this.toasts.filter((t) => t.id !== id);
    this.notify(["toasts"]);
  }
}

export const store = new Store();
