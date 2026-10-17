// Generic parameter editors, chosen by type kind: they read the value from
// the store and write through it, and are disabled unless writes are armed.

import { useEffect, useState } from "react";
import type { KeyboardEvent } from "react";
import type { TypeInfo } from "../lib/api";
import { isOnOff, prettyText, setBit, stepSize } from "../lib/values";
import { useCanWrite, useLeaf, useValue } from "../store/hooks";
import { store } from "../store/store";
import type { Val } from "../store/store";

export interface ControlProps {
  addr: string;
  /** labels for the two states of an OFF/ON toggle */
  onLabel?: string;
  offLabel?: string;
  /** an OFF/ON field whose ON means "not muted": show MUTE lit when OFF */
  mute?: boolean;
}

/** ParamControl picks the editor for a parameter from its type. */
export function ParamControl(props: ControlProps) {
  const leaf = useLeaf(props.addr);
  const val = useValue(props.addr);
  const can = useCanWrite(props.addr);
  if (!leaf) return <span className="muted">—</span>;
  const t = leaf.field.type;
  const disabled = !can || val === undefined;
  switch (t.kind) {
    case "enum":
      if (isOnOff(t)) return <Toggle {...props} val={val} disabled={disabled} />;
      return <EnumSelect addr={props.addr} t={t} val={val} disabled={disabled} />;
    case "int":
      return <IntInput addr={props.addr} t={t} val={val} disabled={disabled} />;
    case "bitmap":
      return <BitToggles addr={props.addr} t={t} val={val} disabled={disabled} />;
    case "string":
      return <StringInput addr={props.addr} t={t} val={val} disabled={disabled} />;
    case "lin":
    case "log":
    case "level":
      return <NumSlider addr={props.addr} t={t} val={val} disabled={disabled} />;
  }
  return <code className="readonly">{val?.text ?? ""}</code>;
}

interface EditorProps {
  addr: string;
  t: TypeInfo;
  val: Val | undefined;
  disabled: boolean;
}

function Toggle({ addr, val, disabled, onLabel, offLabel, mute }: ControlProps & { val: Val | undefined; disabled: boolean }) {
  const on = Number(val?.value) === 1;
  if (mute) {
    return (
      <button
        type="button"
        className={"btn mute" + (on ? "" : " lit")}
        aria-pressed={!on}
        disabled={disabled}
        onClick={() => store.set(addr, on ? 0 : 1)}
      >
        MUTE
      </button>
    );
  }
  return (
    <button
      type="button"
      className={"btn toggle" + (on ? " lit" : "")}
      aria-pressed={on}
      disabled={disabled}
      onClick={() => store.set(addr, on ? 0 : 1)}
    >
      {on ? (onLabel ?? "ON") : (offLabel ?? "OFF")}
    </button>
  );
}

function EnumSelect({ addr, t, val, disabled }: EditorProps) {
  return (
    <select
      className="select"
      value={val === undefined ? "" : String(val.value)}
      disabled={disabled}
      onChange={(e) => store.set(addr, Number(e.target.value))}
    >
      {val === undefined && <option value="">—</option>}
      {(t.names ?? []).map((n, i) => (
        <option key={i} value={i}>
          {n}
        </option>
      ))}
    </select>
  );
}

/** useDraft keeps what is typed in a field until it is committed (Enter or blur). */
function useDraft(current: string) {
  const [draft, setDraft] = useState<string | null>(null);
  useEffect(() => setDraft(null), [current]);
  return [draft ?? current, draft !== null, setDraft] as const;
}

function IntInput({ addr, t, val, disabled }: EditorProps) {
  const current = val === undefined ? "" : String(val.value);
  const [text, dirty, setDraft] = useDraft(current);
  const commit = () => {
    if (!dirty) return;
    const n = Math.round(Number(text));
    if (text.trim() === "" || Number.isNaN(n)) return setDraft(null);
    store.set(addr, Math.min(t.max ?? n, Math.max(t.min ?? n, n)));
    setDraft(null);
  };
  return (
    <input
      className="input num"
      type="number"
      inputMode="numeric"
      min={t.min}
      max={t.max}
      value={text}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => onEnter(e, commit, () => setDraft(null))}
    />
  );
}

function StringInput({ addr, t, val, disabled }: EditorProps) {
  const current = val === undefined ? "" : String(val.value);
  const [text, dirty, setDraft] = useDraft(current);
  const commit = () => {
    if (dirty && text !== current) store.set(addr, text);
    setDraft(null);
  };
  return (
    <input
      className="input"
      type="text"
      maxLength={t.max}
      value={text}
      disabled={disabled}
      spellCheck={false}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => onEnter(e, commit, () => setDraft(null))}
    />
  );
}

function onEnter(e: KeyboardEvent<HTMLInputElement>, commit: () => void, cancel: () => void) {
  if (e.key === "Enter") {
    commit();
    e.currentTarget.blur();
  } else if (e.key === "Escape") {
    cancel();
    e.currentTarget.blur();
  }
}

function BitToggles({ addr, t, val, disabled }: EditorProps) {
  const bits = t.bits ?? 0;
  const v = Number(val?.value ?? 0);
  return (
    <div className="bits" role="group">
      {Array.from({ length: bits }, (_, i) => {
        const on = ((v >>> i) & 1) === 1;
        return (
          <button
            key={i}
            type="button"
            className={"btn bit" + (on ? " lit" : "")}
            aria-pressed={on}
            aria-label={`bit ${i + 1}`}
            disabled={disabled}
            onClick={() => store.set(addr, setBit(v, i, !on))}
          >
            {i + 1}
          </button>
        );
      })}
    </div>
  );
}

/** unitOf prints a type's unit after a value. */
export const unitOf = (t: TypeInfo) => (t.unit ? " " + t.unit : "");

function NumSlider({ addr, t, val, disabled }: EditorProps) {
  const step = stepSize(t.steps) || "any";
  const text = prettyText(val?.text);
  return (
    <div className="numslider">
      <input
        type="range"
        min={0}
        max={1}
        step={step}
        value={Number(val?.value ?? 0)}
        disabled={disabled}
        aria-label={addr}
        aria-valuetext={text + unitOf(t)}
        onChange={(e) => store.set(addr, Number(e.target.value))}
        onPointerUp={() => store.flush(addr)}
        onKeyUp={() => store.flush(addr)}
      />
      <output className="value">
        {text}
        <span className="unit">{t.unit ?? ""}</span>
      </output>
    </div>
  );
}

/** TextSetter sends node text as typed on the console ("-6", "GATE", "1k20"). */
export function TextSetter({ addr }: { addr: string }) {
  const can = useCanWrite(addr);
  const [text, setText] = useState("");
  const send = async () => {
    if (!text.trim()) return;
    if (await store.setText(addr, text)) setText("");
  };
  return (
    <form
      className="textset"
      onSubmit={(e) => {
        e.preventDefault();
        void send();
      }}
    >
      <input
        className="input"
        type="text"
        placeholder="node text"
        value={text}
        disabled={!can}
        spellCheck={false}
        aria-label={`${addr} as node text`}
        onChange={(e) => setText(e.target.value)}
      />
      <button type="submit" className="btn" disabled={!can || !text.trim()}>
        Set
      </button>
    </form>
  );
}
