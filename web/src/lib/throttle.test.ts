import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KeyedThrottle, createThrottle } from "./throttle";

describe("throttle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends the first value at once and the last one on the trailing edge", () => {
    const sent: number[] = [];
    const t = createThrottle((v: number) => sent.push(v), 33);
    t.push(1);
    expect(sent).toEqual([1]);
    t.push(2);
    t.push(3);
    t.push(4);
    expect(sent).toEqual([1]);
    vi.advanceTimersByTime(33);
    expect(sent).toEqual([1, 4]);
    vi.advanceTimersByTime(100);
    expect(sent).toEqual([1, 4]);
  });

  it("limits the rate of a continuous drag to about 30 per second", () => {
    const sent: number[] = [];
    const t = createThrottle((v: number) => sent.push(v), 33);
    for (let i = 0; i < 1000; i++) {
      t.push(i);
      vi.advanceTimersByTime(1); // a pointer event every millisecond for a second
    }
    vi.advanceTimersByTime(50);
    expect(sent.length).toBeGreaterThanOrEqual(29);
    expect(sent.length).toBeLessThanOrEqual(32);
    expect(sent[sent.length - 1]).toBe(999);
  });

  it("flushes and cancels", () => {
    const sent: number[] = [];
    const t = createThrottle((v: number) => sent.push(v), 33);
    t.push(1);
    t.push(2);
    expect(t.pending()).toBe(true);
    t.flush();
    expect(sent).toEqual([1, 2]);
    t.push(3);
    t.cancel();
    vi.advanceTimersByTime(100);
    expect(sent).toEqual([1, 2]);
  });

  it("keeps one throttle per key", () => {
    const sent: string[] = [];
    const k = new KeyedThrottle<number>((key, v) => sent.push(`${key}=${v}`), 33);
    k.push("a", 1);
    k.push("b", 1);
    k.push("a", 2);
    expect(sent).toEqual(["a=1", "b=1"]);
    vi.advanceTimersByTime(40);
    expect(sent).toEqual(["a=1", "b=1", "a=2"]);
  });
});
