import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SchemaResponse, SetBody, StateNode, Status } from "../lib/api";
import { ApiError } from "../lib/api";
import { HOLD, Store } from "./store";
import type { Api } from "./store";

const SCHEMA: SchemaResponse = {
  model: "M32C",
  types: [
    { kind: "enum", describe: "enum {OFF, ON}", names: ["OFF", "ON"] },
    { kind: "level", describe: "level [-oo, +10, 1024] dB", steps: 1024, unit: "dB" },
    { kind: "lin", describe: "linf [-100, 100, 2]", min: -100, max: 100, steps: 101 },
    { kind: "string", describe: "string (max 12)", max: 12 },
    { kind: "enum", describe: "enum {HALL, DLY}", names: ["HALL", "DLY"] },
    { kind: "lin", describe: "linf [0, 1]", min: 0, max: 1 },
  ],
  nodes: [
    { path: "/ch/01/config", scene: true, fields: [{ name: "name", type: 3 }] },
    {
      path: "/ch/01/mix",
      scene: true,
      fields: [
        { name: "on", type: 0 },
        { name: "fader", type: 1 },
        { name: "pan", type: 2 },
      ],
    },
    {
      path: "/ch/01/mix/03",
      scene: true,
      fields: [
        { name: "on", type: 0 },
        { name: "level", type: 1 },
      ],
    },
    { path: "/fx/1", scene: true, fields: [{ name: "type", type: 4 }] },
    { path: "/fx/1/par", scene: true, variant: "/fx/1/type", fields: [{ name: "01", type: 5 }] },
  ],
};

const STATE: StateNode[] = [
  { path: "/ch/01/config", line: '/ch/01/config "Hang"', values: [{ addr: "/ch/01/config/name", text: '"Hang"', value: "Hang" }] },
  {
    path: "/ch/01/mix",
    line: "/ch/01/mix ON   -oo +0",
    values: [
      { addr: "/ch/01/mix/on", text: "ON", value: 1 },
      { addr: "/ch/01/mix/fader", text: "  -oo", value: 0 },
      { addr: "/ch/01/mix/pan", text: "+0", value: 0.5 },
    ],
  },
  { path: "/fx/1/par", line: "/fx/1/par 20 1.57", values: null, text: true },
];

const CONNECTED: Status = {
  connected: true,
  connecting: false,
  console: {
    addr: "127.0.0.1:10023",
    info: { server_version: "", server_name: "", model: "M32C", firmware: "4.06", ip: "", name: "emu" },
    connected: true,
    synced: 4,
    total: 4,
    armed: true,
    last_seen: "",
  },
};

function fakeApi() {
  const sets: SetBody[] = [];
  let fail: Error | null = null;
  const states: Record<string, StateNode[]> = {};
  const api = {
    schema: async () => SCHEMA,
    state: async (prefix?: string) => (prefix ? (states[prefix] ?? []) : STATE),
    set: async (b: SetBody) => {
      sets.push(b);
      if (fail) throw fail;
    },
  } as unknown as Api;
  return { api, sets, states, failWith: (e: Error | null) => (fail = e) };
}

let now = 0;

const ENGINEER = { role: "engineer", local: true, link: false, musicians_can_write: true } as const;

async function ready() {
  const f = fakeApi();
  const s = new Store(f.api, () => now);
  await s.loadSchema();
  s.setAccess(ENGINEER);
  s.setStatus(CONNECTED);
  await s.reloadState();
  return { s, ...f };
}

beforeEach(() => {
  vi.useFakeTimers();
  now = 1000;
});
afterEach(() => {
  vi.useRealTimers();
});

describe("store", () => {
  it("loads the state and composes node lines", async () => {
    const { s } = await ready();
    expect(s.getValue("/ch/01/mix/fader")).toEqual({ value: 0, text: "  -oo" });
    expect(s.lineOf("/ch/01/mix")).toBe("/ch/01/mix ON   -oo +0");
    expect(s.lineOf("/fx/1/par")).toBe("/fx/1/par 20 1.57");
    expect(s.leaf("/ch/01/mix/pan")?.field.type.kind).toBe("lin");
  });

  it("applies change events and notifies the subscribers of the address and the node", async () => {
    const { s } = await ready();
    const onFader = vi.fn();
    const onNode = vi.fn();
    const onPan = vi.fn();
    s.subscribe("v:/ch/01/mix/fader", onFader);
    s.subscribe("n:/ch/01/mix", onNode);
    s.subscribe("v:/ch/01/mix/pan", onPan);
    s.applyChanges([{ addr: "/ch/01/mix/fader", text: "-10.0", value: 0.5, origin: "console" }]);
    expect(onFader).toHaveBeenCalledTimes(1);
    expect(onNode).toHaveBeenCalledTimes(1);
    expect(onPan).not.toHaveBeenCalled();
    expect(s.lineOf("/ch/01/mix")).toBe("/ch/01/mix ON -10.0 +0");
  });

  it("shows a write at once, throttles floats and holds stale echoes back", async () => {
    const { s, sets } = await ready();
    s.set("/ch/01/mix/fader", 0.3);
    expect(sets).toHaveLength(1);
    expect(s.getValue("/ch/01/mix/fader")?.pending).toBe(true);
    s.set("/ch/01/mix/fader", 0.4);
    s.set("/ch/01/mix/fader", 0.5);
    expect(sets).toHaveLength(1); // throttled
    expect(s.getValue("/ch/01/mix/fader")?.value).toBeCloseTo(0.5, 3);

    // the echo of the first write comes back while dragging: ignored
    s.applyChanges([{ addr: "/ch/01/mix/fader", text: "-38.0", value: 0.3, origin: "local" }]);
    expect(s.getValue("/ch/01/mix/fader")?.value).toBeCloseTo(0.5, 3);

    vi.advanceTimersByTime(40);
    expect(sets).toHaveLength(2);
    expect((sets[1] as { value: number }).value).toBeCloseTo(0.5, 3);

    // the echo of the last write confirms it
    s.applyChanges([{ addr: "/ch/01/mix/fader", text: "-10.0", value: 0.5004887580871582, origin: "local" }]);
    expect(s.getValue("/ch/01/mix/fader")).toEqual({ value: 0.5004887580871582, text: "-10.0" });
  });

  it("quantizes floats and formats the local text", async () => {
    const { s, sets } = await ready();
    s.set("/ch/01/mix/pan", 0.7512);
    expect((sets[0] as { value: number }).value).toBe(0.75);
    expect(s.getValue("/ch/01/mix/pan")?.text).toBe("+50");
    s.set("/ch/01/mix/on", 0);
    expect(s.lineOf("/ch/01/mix")).toBe("/ch/01/mix OFF   -oo +50");
    s.set("/ch/01/config/name", "Hang 2");
    expect(s.getValue("/ch/01/config/name")?.text).toBe('"Hang 2"');
  });

  it("applies the last held-back value when the hold time passes", async () => {
    const { s } = await ready();
    s.set("/ch/01/mix/on", 0);
    s.applyChanges([{ addr: "/ch/01/mix/on", text: "ON", value: 1, origin: "console" }]);
    expect(s.getValue("/ch/01/mix/on")?.value).toBe(0);
    now += HOLD + 1;
    s.sweep();
    expect(s.getValue("/ch/01/mix/on")).toEqual({ value: 1, text: "ON" });
  });

  it("reverts a refused write", async () => {
    const { s, failWith } = await ready();
    failWith(new ApiError(403, "writes are not armed"));
    s.set("/ch/01/mix/on", 0);
    expect(s.getValue("/ch/01/mix/on")?.value).toBe(0);
    await vi.advanceTimersByTimeAsync(1); // let the refused request settle
    expect(s.getValue("/ch/01/mix/on")).toEqual({ value: 1, text: "ON" });
    expect(s.toasts.at(-1)?.text).toContain("writes are not armed");
  });

  it("refuses to write when not armed", async () => {
    const { s, sets } = await ready();
    s.setStatus({ ...CONNECTED, console: { ...CONNECTED.console!, armed: false } });
    s.set("/ch/01/mix/on", 0);
    expect(sets).toHaveLength(0);
    expect(s.getValue("/ch/01/mix/on")?.value).toBe(1);
  });

  it("lets a musician write their own mix only, armed or not", async () => {
    const { s, sets } = await ready();
    s.setAccess({ role: "musician", name: "Remy", bus: 3, buses: [3], local: false, link: true, musicians_can_write: true });
    s.setStatus({ ...CONNECTED, console: { ...CONNECTED.console!, armed: false } });
    s.set("/ch/01/mix/03/on", 0);
    expect(sets).toEqual([{ addr: "/ch/01/mix/03/on", value: 0 }]);
    s.set("/ch/01/mix/fader", 0.5);
    expect(sets).toHaveLength(1);
    expect(s.toasts.at(-1)?.text).toContain("own mix");
    s.setAccess({ ...s.access!, musicians_can_write: false });
    s.set("/ch/01/mix/03/on", 1);
    expect(sets).toHaveLength(1);
    expect(s.toasts.at(-1)?.text).toContain("has not let musicians");
  });

  it("buffers changes that arrive while the state loads", async () => {
    const f = fakeApi();
    const s = new Store(f.api, () => now);
    await s.loadSchema();
    s.setStatus(CONNECTED);
    const loading = s.reloadState();
    s.applyChanges([{ addr: "/ch/01/mix/fader", text: "-10.0", value: 0.5, origin: "console" }]);
    await loading;
    expect(s.getValue("/ch/01/mix/fader")?.text).toBe("-10.0");
  });

  it("reloads a variant node after its selector changed", async () => {
    const { s, states } = await ready();
    states["/fx/1/par"] = [
      {
        path: "/fx/1/par",
        line: "/fx/1/par 0.50",
        values: [{ addr: "/fx/1/par/01", text: "0.50", value: 0.5 }],
        fields: [{ name: "01", label: "Pre Delay", type: -1, type_info: { kind: "lin", describe: "linf [0, 1]", min: 0, max: 1 } }],
      },
    ];
    s.applyChanges([{ addr: "/fx/1/type", text: "HALL", value: 0, origin: "local" }]);
    await vi.advanceTimersByTimeAsync(300);
    expect(s.getNode("/fx/1/par")?.text).toBeUndefined();
    expect(s.leaf("/fx/1/par/01")?.field.label).toBe("Pre Delay");
    expect(s.lineOf("/fx/1/par")).toBe("/fx/1/par 0.50");
  });

  it("clears the values when a new connection starts", async () => {
    const { s } = await ready();
    s.setStatus({ connected: false, connecting: true });
    expect(s.getValue("/ch/01/mix/on")).toBeUndefined();
    expect(s.stateLoaded).toBe(false);
  });
});
