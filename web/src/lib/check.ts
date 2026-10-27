// The pre-show check (GET /api/check): what can spoil a recording or a
// show, each line green, amber, red or grey, with what to do.

import { call } from "./api";

export type CheckLevel = "ok" | "warn" | "bad" | "unknown" | "info";

export interface CheckLine {
  /** console, link, card, rate, device, disk, power, sleep, usb, phones, busy */
  id: string;
  title: string;
  level: CheckLevel;
  /** what is found */
  text: string;
  /** what to do about it */
  fix?: string;
}

export interface CheckView {
  lines: CheckLine[];
  /** the show length the disk is checked for */
  hours: number;
  at: string;
}

export const checkApi = {
  get: (hours: number) => call<CheckView>("GET", `/api/check?hours=${encodeURIComponent(hours)}`),
};

/** CHECK_HOURS is where the browser keeps the planned show length; 3 h unless set. */
export const CHECK_HOURS = "tabula.check.hours";

export interface CheckSummary {
  bad: number;
  warn: number;
  /** the worst level found */
  level: CheckLevel;
  /** "all good", "2 to fix, 1 to look at" */
  text: string;
}

/** checkSummary counts what is wrong: red to fix, amber to look at. */
export function checkSummary(v: CheckView): CheckSummary {
  const bad = v.lines.filter((l) => l.level === "bad").length;
  const warn = v.lines.filter((l) => l.level === "warn").length;
  const parts = [bad && `${bad} to fix`, warn && `${warn} to look at`].filter(Boolean);
  return { bad, warn, level: bad ? "bad" : warn ? "warn" : "ok", text: parts.length ? parts.join(", ") : "all good" };
}

/** LEVEL_LABELS name the levels for screen readers and the legend. */
export const LEVEL_LABELS: Record<CheckLevel, string> = {
  ok: "good",
  warn: "to look at",
  bad: "to fix",
  unknown: "unknown here",
  info: "nothing to check",
};
