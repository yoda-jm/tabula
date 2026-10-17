// A vertical fader on the console's fader law. Dragging is relative (a
// touch never makes the fader jump to the finger), Shift drags finely, and
// the arrow keys move it by small steps.

import { useRef } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import { FADER_MARKS, clamp01, dbToLevel } from "../lib/values";

interface Props {
  value: number; // normalized
  onChange(v: number): void;
  onRelease(): void;
  disabled?: boolean;
  label: string;
  valueText: string;
  steps?: number;
}

export function Fader({ value, onChange, onRelease, disabled, label, valueText, steps = 1024 }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const drag = useRef<{ y: number; v: number; travel: number; id: number } | null>(null);

  const down = (e: PointerEvent<HTMLDivElement>) => {
    if (disabled || !ref.current) return;
    if (e.pointerType === "mouse" && e.button !== 0) return;
    e.preventDefault();
    ref.current.setPointerCapture(e.pointerId);
    ref.current.focus({ preventScroll: true });
    const cap = ref.current.querySelector<HTMLElement>(".fader-cap")?.offsetHeight ?? 0;
    drag.current = { y: e.clientY, v: value, travel: Math.max(40, ref.current.clientHeight - cap), id: e.pointerId };
  };
  const move = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    const fine = e.shiftKey ? 0.2 : 1;
    d.v = clamp01(d.v + ((d.y - e.clientY) / d.travel) * fine);
    d.y = e.clientY;
    onChange(d.v);
  };
  const up = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    drag.current = null;
    onRelease();
  };
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return;
    const small = e.shiftKey ? 1 / (steps - 1) : 0.01;
    const big = 0.05;
    const delta: Record<string, number> = { ArrowUp: small, ArrowDown: -small, PageUp: big, PageDown: -big };
    if (!(e.key in delta)) return;
    e.preventDefault();
    onChange(clamp01(value + delta[e.key]));
    onRelease();
  };

  // the cap's bottom edge travels over the fader height minus the cap
  const pos = (v: number) => `calc(${v} * (100% - var(--cap-h)))`;
  const mark = (v: number) => `calc(${v} * (100% - var(--cap-h)) + var(--cap-h) / 2)`;
  return (
    <div
      ref={ref}
      className={"fader" + (disabled ? " disabled" : "")}
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={1}
      aria-valuenow={Math.round(value * 1000) / 1000}
      aria-valuetext={valueText}
      aria-disabled={disabled}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={up}
      onLostPointerCapture={up}
      onKeyDown={key}
    >
      <div className="fader-scale" aria-hidden>
        {FADER_MARKS.map((m) => (
          <span key={m.label} className={"fader-mark" + (m.db === 0 ? " zero" : "")} style={{ bottom: mark(dbToLevel(m.db)) }}>
            {m.label}
          </span>
        ))}
      </div>
      <div className="fader-track" aria-hidden>
        <div className="fader-fill" style={{ height: `${value * 100}%` }} />
      </div>
      <div className="fader-cap" style={{ bottom: pos(value) }} aria-hidden />
    </div>
  );
}
