import { describe, expect, it } from "vitest";
import { isPhantom, phantomAsk, phantomPort, phantomTarget } from "./phantom";

describe("phantom power", () => {
  it("finds the phantom switches and names their input", () => {
    expect(isPhantom("/headamp/000/phantom")).toBe(true);
    expect(isPhantom("/headamp/000/gain")).toBe(false);
    expect(phantomPort("/headamp/011/phantom")).toBe("Local 12");
    expect(phantomPort("/headamp/032/phantom")).toBe("AES50 A 1");
  });

  it("writes what the question said", () => {
    // taken when the dialog opens: a switch turned on elsewhere meanwhile is still turned off
    const ask = phantomAsk(0);
    expect(ask.title).toBe("Turn phantom power off?");
    expect(ask.target).toBe(0);
    expect(phantomAsk(1)).toMatchObject({ target: 1, confirm: "Turn on +48 V", verb: "Sending" });
  });

  it("reads the text typed for a switch", () => {
    expect(phantomTarget(" on ")).toBe(1);
    expect(phantomTarget("0")).toBe(0);
    expect(phantomTarget("OFF")).toBe(0);
    expect(phantomTarget("48")).toBeNull();
  });
});
