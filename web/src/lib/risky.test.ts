import { describe, expect, it } from "vitest";
import { riskOf } from "./risky";

describe("riskOf", () => {
  it("asks before what can stop a show", () => {
    expect(riskOf("/-prefs/clocksource", 3)?.confirm).toBe("Change the clock");
    expect(riskOf("/-prefs/clockrate", "44K1")?.confirm).toBe("Change the clock");
    expect(riskOf("/-action/setsrate", 1)?.confirm).toBe("Change the clock");
    expect(riskOf("/-prefs/ip/dhcp", 1)?.confirm).toBe("Change the network settings");
    expect(riskOf("/-prefs/ip/addr/2", 12)?.confirm).toBe("Change the network settings");
    expect(riskOf("/-action/setip", 1)?.confirm).toBe("Change the network settings");
    expect(riskOf("/-prefs/card/USBmode", 1)?.confirm).toBe("Change the card");
    expect(riskOf("/-action/initall", 1)?.title).toBe("Initialize the console?");
    expect(riskOf("/-action/formatcard", "1")?.confirm).toBe("Format");
    expect(riskOf("/-stat/lock", 2)?.title).toBe("Shut the console down?");
    expect(riskOf("/-stat/lock", "1")?.title).toBe("Lock the console?");
    expect(riskOf("/-action/goscene", 0)?.title).toBe("Recall scene 0?");
    expect(riskOf("/-action/gosnippet", "12")?.body).toContain("backup");
  });

  it("lets the rest go", () => {
    expect(riskOf("/-action/initall", 0)).toBeNull();
    expect(riskOf("/-stat/lock", 0)).toBeNull();
    expect(riskOf("/-action/setrtasrc", 3)).toBeNull();
    expect(riskOf("/-action/goscene", "x")).toBeNull();
    expect(riskOf("/ch/01/mix/fader", 0.75)).toBeNull();
    expect(riskOf("/-prefs/name", "M32C")).toBeNull();
    expect(riskOf("/-prefs/ipx", 1)).toBeNull();
  });
});
