import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SchemaResponse, StateNode, Status } from "../lib/api";
import { SWAP_DELAY, startEvents } from "./events";
import { Store } from "./store";
import type { Api } from "./store";

// The stream's own fetches (status, record) never answer here.
vi.mock("../lib/api", async (orig) => {
  const mod = await orig<typeof import("../lib/api")>();
  return { ...mod, api: { ...mod.api, status: () => new Promise(() => {}) } };
});
vi.mock("../lib/session", async (orig) => {
  const mod = await orig<typeof import("../lib/session")>();
  return { ...mod, sessionApi: { ...mod.sessionApi, record: () => new Promise(() => {}) } };
});

/** A fake EventSource: tests fire its events by hand. */
class FakeSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  static all: FakeSource[] = [];
  readyState = FakeSource.CONNECTING;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private handlers = new Map<string, ((e: { data: string }) => void)[]>();
  constructor(public url: string) {
    FakeSource.all.push(this);
  }
  addEventListener(type: string, fn: (e: { data: string }) => void) {
    this.handlers.set(type, [...(this.handlers.get(type) ?? []), fn]);
  }
  close() {
    this.readyState = FakeSource.CLOSED;
  }
  open() {
    this.readyState = FakeSource.OPEN;
    this.onopen?.();
  }
  fire(type: string, data: unknown) {
    for (const fn of this.handlers.get(type) ?? []) fn({ data: JSON.stringify(data) });
  }
}

const SCHEMA: SchemaResponse = {
  model: "M32C",
  types: [{ kind: "level", describe: "level", steps: 1024, unit: "dB" }],
  nodes: [{ path: "/ch/01/mix", scene: true, fields: [{ name: "fader", type: 0 }] }],
};
const STATE: StateNode[] = [
  { path: "/ch/01/mix", line: "/ch/01/mix -oo", values: [{ addr: "/ch/01/mix/fader", text: "  -oo", value: 0 }] },
];
const CONNECTED: Status = {
  connected: true,
  connecting: false,
  console: {
    addr: "127.0.0.1:10023",
    info: { server_version: "", server_name: "", model: "M32C", firmware: "4.06", ip: "", name: "emu" },
    connected: true,
    synced: 1,
    total: 1,
    armed: false,
    last_seen: "",
  },
};

let loads = 0;
const fakeApi = {
  schema: async () => SCHEMA,
  state: async () => {
    loads++;
    return STATE;
  },
} as unknown as Api;

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("EventSource", FakeSource);
  FakeSource.all = [];
  loads = 0;
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("event stream", () => {
  it("reopens with the wanted meter groups without a gap or a reload", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    const stop = startEvents(store);
    const first = FakeSource.all[0];
    expect(first.url).toBe("/api/events?meters=0,2");
    first.open();
    first.fire("status", CONNECTED);
    await vi.runOnlyPendingTimersAsync();
    expect(loads).toBe(1);

    const release = store.wantMeters("15");
    store.wantMeters("15")(); // a second user that leaves at once changes nothing
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    expect(FakeSource.all).toHaveLength(2);
    const second = FakeSource.all[1];
    expect(second.url).toBe("/api/events?meters=0,2,15");

    // until the new stream has started, the old one still counts
    first.fire("change", [{ addr: "/ch/01/mix/fader", text: "-10.0", value: 0.5, origin: "console" }]);
    expect(store.getValue("/ch/01/mix/fader")?.value).toBe(0.5);
    expect(first.readyState).toBe(FakeSource.OPEN);

    second.open();
    second.fire("status", CONNECTED);
    expect(first.readyState).toBe(FakeSource.CLOSED);
    first.fire("change", [{ addr: "/ch/01/mix/fader", text: "  -oo", value: 0, origin: "console" }]);
    expect(store.getValue("/ch/01/mix/fader")?.value).toBe(0.5); // closed: ignored
    second.fire("meters", { id: "15", values: [-30, -40] });
    expect(store.getMeters("15")).toEqual([-30, -40]);
    await vi.runOnlyPendingTimersAsync();
    expect(loads).toBe(1); // nothing was missed: no reload

    release();
    expect(store.getMeters("15")).toBeUndefined();
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    expect(FakeSource.all[2].url).toBe("/api/events?meters=0,2");
    stop();
    expect(FakeSource.all.every((s) => s.readyState === FakeSource.CLOSED)).toBe(true);
  });

  it("reloads when the stream it replaces was down", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    startEvents(store);
    const first = FakeSource.all[0];
    first.open();
    first.fire("status", CONNECTED);
    first.onerror?.(); // dropped, the browser reconnects by itself
    store.wantMeters("15");
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    const second = FakeSource.all[1];
    second.open(); // not the stream in use yet: ignored
    second.fire("status", CONNECTED);
    await vi.runOnlyPendingTimersAsync();
    expect(loads).toBe(2);
    expect(store.stream).toBe("open");
  });
});
