// The mixer: banks of strips with scribble strip, meter, fader, mute and pan.

import { Fader } from "../components/Fader";
import { StripMeter } from "../components/Meter";
import { Scribble } from "../components/Scribble";
import { usePersistent } from "../lib/route";
import type { Strip } from "../lib/strips";
import { BANKS, stripAddrs } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useCanWrite, useLeaf, useSchema, useValue } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

export function MixerPage({ selected, onSelect }: { selected?: string; onSelect(path: string): void }) {
  const [bankId, setBank] = usePersistent("tabula.bank", BANKS[0].id);
  const bank = BANKS.find((b) => b.id === bankId) ?? BANKS[0];
  return (
    <div className="page mixer">
      <div className="banks" role="tablist" aria-label="Banks">
        {BANKS.map((b) => (
          <button
            key={b.id}
            type="button"
            role="tab"
            aria-selected={b.id === bank.id}
            className={"bank" + (b.id === bank.id ? " active" : "")}
            onClick={() => setBank(b.id)}
          >
            {b.label}
          </button>
        ))}
      </div>
      <NotConnected />
      <div className="strips" role="tabpanel" aria-label={bank.label}>
        {bank.strips.map((s) => (
          <StripView key={s.path} strip={s} selected={s.path === selected} onSelect={() => onSelect(s.path)} />
        ))}
      </div>
    </div>
  );
}

function StripView({ strip, selected, onSelect }: { strip: Strip; selected: boolean; onSelect(): void }) {
  const schema = useSchema();
  const can = useCanWrite();
  if (!schema) return null;
  const a = stripAddrs(schema, strip);
  return (
    <div className={"strip" + (selected ? " selected" : "")}>
      <Scribble strip={strip} nameAddr={a.name} colorAddr={a.color} selected={selected} onClick={onSelect} />
      {a.pan ? <Pan addr={a.pan} disabled={!can} /> : <div className="pan-slot" />}
      <div className="strip-fader">
        <StripMeter strip={strip} />
        {a.fader && <StripFader addr={a.fader} label={strip.label} disabled={!can} />}
      </div>
      {a.on && <Mute addr={a.on} disabled={!can} />}
    </div>
  );
}

function StripFader({ addr, label, disabled }: { addr: string; label: string; disabled: boolean }) {
  const val = useValue(addr);
  const leaf = useLeaf(addr);
  const text = prettyText(val?.text);
  return (
    <div className="fader-wrap">
      <Fader
        value={Number(val?.value ?? 0)}
        steps={leaf?.field.type.steps}
        disabled={disabled || val === undefined}
        label={`${label} fader`}
        valueText={`${text} dB`}
        onChange={(v) => store.set(addr, v)}
        onRelease={() => store.flush(addr)}
      />
      <output className={"db" + (val?.pending ? " pending" : "")}>{val ? text : "—"}</output>
    </div>
  );
}

function Mute({ addr, disabled }: { addr: string; disabled: boolean }) {
  const val = useValue(addr);
  const muted = val !== undefined && Number(val.value) === 0;
  return (
    <button
      type="button"
      className={"btn mute" + (muted ? " lit" : "")}
      aria-pressed={muted}
      disabled={disabled || val === undefined}
      onClick={() => store.set(addr, muted ? 1 : 0)}
    >
      MUTE
    </button>
  );
}

function Pan({ addr, disabled }: { addr: string; disabled: boolean }) {
  const val = useValue(addr);
  const leaf = useLeaf(addr);
  const steps = leaf?.field.type.steps ?? 101;
  const text = prettyText(val?.text);
  const shown = text === "+0" || text === "0" ? "C" : text.startsWith("-") ? "L" + text.slice(1) : text ? "R" + text.replace("+", "") : "—";
  return (
    <label className="pan">
      <input
        type="range"
        min={0}
        max={1}
        step={1 / (steps - 1)}
        value={Number(val?.value ?? 0.5)}
        disabled={disabled || val === undefined}
        aria-label="Pan"
        aria-valuetext={shown}
        onChange={(e) => store.set(addr, Number(e.target.value))}
        onPointerUp={() => store.flush(addr)}
        onDoubleClick={() => !disabled && store.set(addr, 0.5)}
      />
      <span className="pan-text">{shown}</span>
    </label>
  );
}
