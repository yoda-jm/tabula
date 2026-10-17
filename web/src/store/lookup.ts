// The store as a Lookup for the routing and library helpers (lib/routing.ts).

import type { Lookup } from "../lib/routing";
import { store } from "./store";

export const lookup: Lookup = {
  text: (addr) => store.getValue(addr)?.text,
  value: (addr) => store.getValue(addr)?.value,
};
