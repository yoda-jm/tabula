// Small helpers for the console's numbering.

/** two pads to two digits, as in /ch/01; three to three, as in /headamp/043. */
export const two = (n: number) => String(n).padStart(2, "0");
export const three = (n: number) => String(n).padStart(3, "0");

/** range lists the integers from `from` to `to`, both included. */
export const range = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => from + i);
