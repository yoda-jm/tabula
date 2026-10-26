import { describe, expect, it, vi } from "vitest";
import type { Lock } from "./wakelock";
import { keepAwake } from "./wakelock";

/** A fake wake lock API and page: tests release locks and hide or show the page by hand. */
function fakes() {
  const locks: (Lock & { released: boolean; drop(): void })[] = [];
  const locker = {
    request: vi.fn(async () => {
      const listeners: (() => void)[] = [];
      const l = {
        released: false,
        release: async () => {
          l.released = true;
        },
        addEventListener: (_: "release", fn: () => void) => listeners.push(fn),
        drop: () => listeners.forEach((fn) => fn()), // the browser releases it (page hidden)
      };
      locks.push(l);
      return l;
    }),
  };
  const handlers = new Set<() => void>();
  const page = {
    visibilityState: "visible",
    addEventListener: (_: "visibilitychange", fn: () => void) => handlers.add(fn),
    removeEventListener: (_: "visibilitychange", fn: () => void) => handlers.delete(fn),
    show(state: string) {
      page.visibilityState = state;
      handlers.forEach((fn) => fn());
    },
  };
  return { locks, locker, page };
}

describe("wake lock", () => {
  it("holds the lock, takes it again when the page shows, and lets it go when stopped", async () => {
    const { locks, locker, page } = fakes();
    const active = vi.fn();
    const stop = keepAwake(locker, page, active);
    await Promise.resolve();
    expect(locker.request).toHaveBeenCalledTimes(1);
    expect(active).toHaveBeenLastCalledWith(true);

    page.show("visible"); // already held: no second lock
    expect(locker.request).toHaveBeenCalledTimes(1);
    locks[0].drop();
    page.show("hidden");
    expect(active).toHaveBeenLastCalledWith(false);
    page.show("visible");
    await Promise.resolve();
    expect(locker.request).toHaveBeenCalledTimes(2);
    expect(active).toHaveBeenLastCalledWith(true);

    stop();
    expect(locks[1].released).toBe(true);
    page.show("visible");
    expect(locker.request).toHaveBeenCalledTimes(2);
  });

  it("gives back a lock granted after it was stopped", async () => {
    const { locks, locker, page } = fakes();
    const stop = keepAwake(locker, page, () => {});
    stop();
    await Promise.resolve();
    expect(locks[0].released).toBe(true);
  });
});
