import { describe, expect, it } from "vitest";
import { linkQuality } from "./link";

describe("linkQuality", () => {
  it("is fine on a quiet network", () => {
    const q = linkQuality({ rtt_ms: 3.42, asked: 100, lost: 0, caught: 0 });
    expect(q).toEqual({ level: "ok", short: "3.4 ms", detail: "3.4 ms round trip · no request unanswered (of the last 100)" });
  });

  it("warns, then alarms, on a weak one", () => {
    expect(linkQuality({ rtt_ms: 12, asked: 100, lost: 4, caught: 0 })).toMatchObject({ level: "warn", short: "4% lost" });
    expect(linkQuality({ rtt_ms: 95, asked: 100, lost: 0, caught: 0 })).toMatchObject({ level: "warn", short: "95 ms" });
    expect(linkQuality({ rtt_ms: 40, asked: 20, lost: 5, caught: 0 })).toMatchObject({ level: "bad", short: "25% lost" });
  });

  it("tells the changes found again", () => {
    const q = linkQuality({ rtt_ms: 5, asked: 100, lost: 1, caught: 2, last_caught: "/ch/01/mix/fader  -3.0" });
    expect(q.detail).toBe(
      "5.0 ms round trip · 1 of the last 100 requests unanswered · 2 changes lost on the network found again by reading the console " +
        "back (the last: /ch/01/mix/fader  -3.0)",
    );
  });
});
