// Keeping a phone's screen on while a musician's mix is open (the Screen
// Wake Lock API). The browser releases the lock whenever the page is hidden:
// it is taken again when the page shows.

export interface Lock {
  release(): Promise<void>;
  addEventListener(type: "release", fn: () => void): void;
}

export interface Locker {
  request(type: "screen"): Promise<Lock>;
}

export interface Page {
  visibilityState: string;
  addEventListener(type: "visibilitychange", fn: () => void): void;
  removeEventListener(type: "visibilitychange", fn: () => void): void;
}

/** wakeLockSupported tells whether this browser can keep the screen on. */
export const wakeLockSupported = () => typeof navigator !== "undefined" && "wakeLock" in navigator;

/**
 * keepAwake holds a screen wake lock, taken again each time the page shows,
 * until the function it returns is called. onActive follows whether a lock
 * is held (the browser may refuse one: no HTTPS, no user gesture yet).
 */
export function keepAwake(locker: Locker, page: Page, onActive: (active: boolean) => void): () => void {
  let stopped = false;
  let held: Lock | null = null;
  const request = () =>
    locker.request("screen").then(
      (l) => {
        if (stopped) {
          void l.release();
          return;
        }
        held = l;
        onActive(true);
        l.addEventListener("release", () => {
          if (held !== l) return;
          held = null;
          onActive(false);
        });
      },
      () => onActive(false),
    );
  void request();
  const onVisible = () => {
    if (page.visibilityState === "visible" && !held) void request();
  };
  page.addEventListener("visibilitychange", onVisible);
  return () => {
    stopped = true;
    page.removeEventListener("visibilitychange", onVisible);
    const l = held;
    held = null;
    void l?.release();
    onActive(false);
  };
}
