import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SchemaResponse, StateNode, Status } from "../lib/api";
import { QUIET, SWAP_DELAY, startEvents } from "./events";
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
    await vi.advanceTimersByTimeAsync(1); // the reload settles
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
    await vi.advanceTimersByTimeAsync(1000);
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
    await vi.advanceTimersByTimeAsync(1);
    expect(loads).toBe(2);
    expect(store.stream).toBe("open");
  });

  it("tries a failed replacement again", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    startEvents(store);
    FakeSource.all[0].open();
    FakeSource.all[0].fire("status", CONNECTED);
    store.wantMeters("15");
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    const failed = FakeSource.all[1];
    failed.onerror?.();
    expect(failed.readyState).toBe(FakeSource.CLOSED);
    await vi.advanceTimersByTimeAsync(1000);
    expect(FakeSource.all).toHaveLength(3);
    expect(FakeSource.all[2].url).toBe("/api/events?meters=0,2,15");
  });

  it("leaves no stream open when a replacement and a reopen cross", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    const stop = startEvents(store);
    const first = FakeSource.all[0];
    first.open();
    first.fire("status", CONNECTED);
    const release = store.wantMeters("15");
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    const second = FakeSource.all[1];
    first.readyState = FakeSource.CLOSED; // the server went away: a reopen is scheduled
    first.onerror?.();
    second.open();
    second.fire("status", CONNECTED); // the replacement wins, the reopen is cancelled
    await vi.advanceTimersByTimeAsync(10000); // the reopen was due after 1 s
    expect(FakeSource.all).toHaveLength(2);
    expect(FakeSource.all.filter((s) => s.readyState !== FakeSource.CLOSED)).toEqual([second]);
    // values of a group given back are dropped until the stream is replaced
    release();
    second.fire("meters", { id: "15", values: [-30] });
    expect(store.getMeters("15")).toBeUndefined();
    stop();
  });

  it("reopens a stream that went silent, through the backoff", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    const stop = startEvents(store);
    const first = FakeSource.all[0];
    first.open();
    first.fire("status", CONNECTED);
    first.fire("meters", { id: "0", values: [0.5] });
    // the server's pings keep it alive
    for (let i = 0; i < 4; i++) {
      await vi.advanceTimersByTimeAsync(5000);
      first.fire("ping", {});
    }
    expect(FakeSource.all).toHaveLength(1);
    expect(store.stream).toBe("open");

    // a half-open connection: no error, no event
    await vi.advanceTimersByTimeAsync(QUIET);
    expect(first.readyState).toBe(FakeSource.CLOSED);
    expect(store.stream).toBe("down");
    expect(store.getMeters("0")).toBeUndefined(); // no frozen meters
    await vi.advanceTimersByTimeAsync(1000);
    expect(FakeSource.all).toHaveLength(2);
    expect(store.stream).toBe("down"); // until the new stream opens
    FakeSource.all[1].open();
    FakeSource.all[1].fire("status", CONNECTED);
    await vi.advanceTimersByTimeAsync(1);
    expect(store.stream).toBe("open");
    expect(loads).toBe(2); // changes may have been missed: reloaded
    stop();
  });

  it("gives up a replacement that never starts", async () => {
    const store = new Store(fakeApi);
    await store.loadSchema();
    startEvents(store);
    const first = FakeSource.all[0];
    first.open();
    first.fire("status", CONNECTED);
    store.wantMeters("15");
    await vi.advanceTimersByTimeAsync(SWAP_DELAY);
    const silent = FakeSource.all[1];
    for (let i = 0; i < 3; i++) {
      await vi.advanceTimersByTimeAsync(5000);
      first.fire("ping", {});
    }
    expect(silent.readyState).toBe(FakeSource.CLOSED);
    expect(first.readyState).toBe(FakeSource.OPEN); // the stream in use stays
    await vi.advanceTimersByTimeAsync(1000);
    expect(FakeSource.all[2].url).toBe("/api/events?meters=0,2,15");
  });
});
