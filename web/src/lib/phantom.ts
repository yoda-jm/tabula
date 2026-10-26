// Phantom power (+48 V) of a headamp: switching it makes a loud pop through
// the PA, so every editor of /headamp/NNN/phantom asks first, and writes
// what the question said, whatever happened to the switch meanwhile.

import { headampPort, portLabel } from "./routing";

const PHANTOM = /^\/headamp\/(\d{3})\/phantom$/;

/** isPhantom tells whether addr is the phantom power switch of a headamp. */
export const isPhantom = (addr: string) => PHANTOM.test(addr);

/** phantomPort names the input of a phantom switch: "Local 12", "AES50 A 3". */
export function phantomPort(addr: string): string {
  const m = PHANTOM.exec(addr);
  const p = m ? headampPort(Number(m[1])) : null;
  return p ? portLabel(p) : addr;
}

/** PhantomAsk is the question asked before switching phantom power, and the value written when confirmed. */
export interface PhantomAsk {
  target: 0 | 1;
  title: string;
  confirm: string;
  /** "Sending" or "Removing" +48 V */
  verb: string;
}

/** phantomAsk is the question for switching phantom power to target: taken when the dialog opens, it is what is written. */
export function phantomAsk(target: 0 | 1): PhantomAsk {
  return target
    ? { target, title: "Turn phantom power on?", confirm: "Turn on +48 V", verb: "Sending" }
    : { target, title: "Turn phantom power off?", confirm: "Turn off", verb: "Removing" };
}

/** phantomTarget reads node text typed for a phantom switch ("ON", "off", "1"): 0, 1, or null when unreadable. */
export function phantomTarget(text: string): 0 | 1 | null {
  const t = text.trim().toUpperCase();
  if (t === "ON" || t === "1") return 1;
  if (t === "OFF" || t === "0") return 0;
  return null;
}
