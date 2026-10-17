import { describe, expect, it } from "vitest";
import type { Whoami } from "./api";
import { atLeast, mayWrite, musicianMay } from "./access";

describe("access", () => {
  it("lets a musician change the sends to their buses and their bus master only", () => {
    expect(musicianMay("/ch/01/mix/03/level", [3])).toBe(true);
    expect(musicianMay("/fxrtn/02/mix/04/on", [3, 4])).toBe(true);
    expect(musicianMay("/bus/03/mix/fader", [3])).toBe(true);
    expect(musicianMay("/ch/01/mix/05/level", [3])).toBe(false);
    expect(musicianMay("/ch/01/mix/03/type", [3])).toBe(false);
    expect(musicianMay("/ch/01/mix/fader", [3])).toBe(false);
    expect(musicianMay("/bus/03/eq/1/g", [3])).toBe(false);
  });

  it("follows the role, the arming and the musicians' permission", () => {
    const musician: Whoami = { role: "musician", bus: 3, buses: [3], local: false, link: true, musicians_can_write: true };
    expect(mayWrite(musician, false, "/ch/01/mix/03/level")).toBe(true);
    expect(mayWrite({ ...musician, musicians_can_write: false }, true, "/ch/01/mix/03/level")).toBe(false);
    const engineer: Whoami = { role: "engineer", local: true, link: false, musicians_can_write: true };
    expect(mayWrite(engineer, false, "/ch/01/mix/fader")).toBe(false);
    expect(mayWrite(engineer, true, "/ch/01/mix/fader")).toBe(true);
    expect(mayWrite({ ...engineer, role: "viewer" }, true, "/ch/01/mix/fader")).toBe(false);
    expect(atLeast("musician", "viewer")).toBe(true);
    expect(atLeast("viewer", "musician")).toBe(false);
  });
});
