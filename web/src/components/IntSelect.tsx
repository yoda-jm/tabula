// A drop-down for int parameters whose values stand for named things
// (output sources, user routing…), disabled unless writes are armed.

import { useCanWrite, useValue } from "../store/hooks";
import { store } from "../store/store";

export interface IntOption {
  value: number;
  label: string;
}

export function IntSelect({ addr, options, label }: { addr: string; options: IntOption[]; label?: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  const current = val === undefined ? "" : String(val.value);
  return (
    <select
      className="select"
      value={current}
      disabled={!can || val === undefined}
      aria-label={label ?? addr}
      onChange={(e) => store.set(addr, Number(e.target.value))}
    >
      {val === undefined && <option value="">—</option>}
      {val !== undefined && !options.some((o) => String(o.value) === current) && <option value={current}>{current}</option>}
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
