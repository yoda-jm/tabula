// A per-key throttle for continuous controls: the first value goes out at
// once, then at most one every `interval` ms, and the last one always goes
// out (trailing edge), so the console ends where the control stopped.

export interface Throttle<T> {
  push(value: T): void;
  /** flush sends the pending value now, if any. */
  flush(): void;
  cancel(): void;
  pending(): boolean;
}

export function createThrottle<T>(send: (value: T) => void, interval: number): Throttle<T> {
  let last = -Infinity;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let queued: { value: T } | null = null;

  const fire = () => {
    timer = null;
    if (!queued) return;
    const { value } = queued;
    queued = null;
    last = Date.now();
    send(value);
  };

  return {
    push(value) {
      queued = { value };
      if (timer) return;
      const wait = last + interval - Date.now();
      if (wait <= 0) fire();
      else timer = setTimeout(fire, wait);
    },
    flush() {
      if (timer) clearTimeout(timer);
      fire();
    },
    cancel() {
      if (timer) clearTimeout(timer);
      timer = null;
      queued = null;
    },
    pending: () => queued !== null,
  };
}

/** A map of throttles, one per key (parameter address). */
export class KeyedThrottle<T> {
  private map = new Map<string, Throttle<T>>();
  constructor(
    private send: (key: string, value: T) => void,
    private interval: number,
  ) {}

  push(key: string, value: T) {
    let t = this.map.get(key);
    if (!t) {
      t = createThrottle((v: T) => this.send(key, v), this.interval);
      this.map.set(key, t);
    }
    t.push(value);
  }

  flush(key: string) {
    this.map.get(key)?.flush();
  }

  cancelAll() {
    for (const t of this.map.values()) t.cancel();
    this.map.clear();
  }
}
