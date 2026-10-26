// The monitor: what is soloed (and Clear solo), the monitor and solo
// settings, talkback A and B, and the oscillator. Solo and the monitor only
// change what the console's monitor and headphone outputs play; talkback
// and the oscillator go to the destinations set here. The oscillator asks
// before it starts: a tone sent to the main mix is heard by the audience.

import type { PointerEvent } from "react";
import { useState } from "react";
import { ConfirmDialog } from "../components/shell";
import { OSC_DESTS, SOLO_ADDRS, TALK_DESTS } from "../lib/monitor";
import { prettyText, setBit } from "../lib/values";
import { useCanWrite, useNodes, useValue } from "../store/hooks";
import { store } from "../store/store";
import { FieldRow, NotConnected, Section } from "./common";

export function MonitorPage() {
  return (
    <div className="page monitor">
      <NotConnected />
      <div className="sections">
        <SoloCard />
        <Section title="Monitor">
          <div className="fields">
            <FieldRow addr="/config/solo/level" label="Level" />
            <FieldRow addr="/config/solo/source" label="Source" />
            <FieldRow addr="/config/solo/sourcetrim" label="Source trim" />
            <FieldRow addr="/config/solo/mono" label="Mono" />
            <FieldRow addr="/config/solo/dim" label="Dim" />
            <FieldRow addr="/config/solo/dimatt" label="Dim by" />
            <FieldRow addr="/config/solo/dimpfl" label="Dim PFL" />
            <FieldRow addr="/config/solo/mute" label="Mute" />
            <FieldRow addr="/config/solo/delay" label="Delay" />
            <FieldRow addr="/config/solo/delaytime" label="Delay time" />
            <FieldRow addr="/config/solo/masterctrl" label="Master control" />
          </div>
        </Section>
        <TalkCard />
        <OscCard />
      </div>
    </div>
  );
}

/** SoloCard lists the strips soloed, each to unsolo, and the solo settings. */
function SoloCard() {
  useNodes(["/-stat/solosw"]);
  const can = useCanWrite();
  const soloed = SOLO_ADDRS.filter((s) => Number(store.getValue(s.addr)?.value) === 1);
  return (
    <section className="card section">
      <header className="section-head">
        <h3>Solo</h3>
        <button type="button" className="btn" disabled={!can || !soloed.length} onClick={() => store.set("/-action/clearsolo", 1)}>
          Clear solo
        </button>
      </header>
      {soloed.length ? (
        <div className="solo-list" role="group" aria-label="Soloed">
          {soloed.map((s) => (
            <SoloChip key={s.addr} addr={s.addr} path={s.strip.path} label={s.strip.label} disabled={!can} />
          ))}
        </div>
      ) : (
        <p className="muted">Nothing is soloed: press SOLO on a strip of the Mixer.</p>
      )}
      <div className="fields">
        <FieldRow addr="/config/solo/exclusive" label="Exclusive (one at a time)" />
        <FieldRow addr="/config/solo/followsel" label="Solo follows select" />
        <FieldRow addr="/config/solo/followsolo" label="Select follows solo" />
        <FieldRow addr="/config/solo/chmode" label="Inputs" />
        <FieldRow addr="/config/solo/busmode" label="Buses" />
        <FieldRow addr="/config/solo/dcamode" label="DCAs" />
      </div>
    </section>
  );
}

function SoloChip({ addr, path, label, disabled }: { addr: string; path: string; label: string; disabled: boolean }) {
  const name = prettyText(useValue(`${path}/config/name`)?.text);
  return (
    <button type="button" className="btn solo lit" disabled={disabled} onClick={() => store.set(addr, 0)} title="Unsolo">
      {label}
      {name ? ` · ${name}` : ""} ✕
    </button>
  );
}

/** TalkCard is talkback A and B: the talk button (held, unless latched), level and destinations. */
function TalkCard() {
  return (
    <section className="card section">
      <header className="section-head">
        <h3>Talkback</h3>
      </header>
      <div className="fields">
        <FieldRow addr="/config/talk/enable" label="Talkback" />
        <FieldRow addr="/config/talk/source" label="Microphone" />
      </div>
      {(["A", "B"] as const).map((ab) => (
        <div key={ab} className="talk">
          <TalkButton ab={ab} />
          <div className="fields">
            <FieldRow addr={`/config/talk/${ab}/level`} label="Level" />
            <FieldRow addr={`/config/talk/${ab}/dim`} label="Dims the monitor" />
            <FieldRow addr={`/config/talk/${ab}/latch`} label="Latch" />
          </div>
          <span className="field-label">Talks to</span>
          <NamedBits addr={`/config/talk/${ab}/destmap`} names={TALK_DESTS} />
        </div>
      ))}
    </section>
  );
}

function TalkButton({ ab }: { ab: "A" | "B" }) {
  const addr = `/-stat/talk/${ab}`;
  const val = useValue(addr);
  const latch = Number(useValue(`/config/talk/${ab}/latch`)?.value) === 1;
  const can = useCanWrite(addr);
  const on = Number(val?.value) === 1;
  const disabled = !can || val === undefined;
  // not latched: talks while held, as on the console
  const hold = (down: boolean) => (e: PointerEvent<HTMLButtonElement>) => {
    if (latch || disabled) return;
    if (down) e.currentTarget.setPointerCapture(e.pointerId);
    store.set(addr, down ? 1 : 0);
  };
  return (
    <button
      type="button"
      className={"btn big talk-btn" + (on ? " lit" : "")}
      aria-pressed={on}
      disabled={disabled}
      onPointerDown={hold(true)}
      onPointerUp={hold(false)}
      onPointerCancel={hold(false)}
      onClick={() => latch && store.set(addr, on ? 0 : 1)}
      onKeyDown={(e) => !latch && (e.key === " " || e.key === "Enter") && !e.repeat && store.set(addr, 1)}
      onKeyUp={(e) => !latch && (e.key === " " || e.key === "Enter") && store.set(addr, 0)}
    >
      TALK {ab}
      <span className="talk-hint">{latch ? "press to turn on and off" : "hold to talk"}</span>
    </button>
  );
}

/** NamedBits edits a bitmap whose bits have names (bit 0 first). */
function NamedBits({ addr, names }: { addr: string; names: string[] }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  const v = Number(val?.value ?? 0);
  return (
    <div className="bits named-bits" role="group">
      {names.map((n, i) => {
        const on = ((v >>> i) & 1) === 1;
        return (
          <button
            key={n}
            type="button"
            className={"btn bit" + (on ? " lit" : "")}
            aria-pressed={on}
            disabled={!can || val === undefined}
            onClick={() => store.set(addr, setBit(v, i, !on))}
          >
            {n}
          </button>
        );
      })}
    </div>
  );
}

/** OscCard is the oscillator: it asks before it starts, saying what goes where. */
function OscCard() {
  const on = useValue("/-stat/osc/on");
  const dest = useValue("/config/osc/dest");
  const type = useValue("/config/osc/type");
  const level = useValue("/config/osc/level");
  const can = useCanWrite("/-stat/osc/on");
  const canDest = useCanWrite("/config/osc/dest");
  const [asking, setAsking] = useState(false);
  const running = Number(on?.value) === 1;
  const where = OSC_DESTS[Number(dest?.value ?? 0)] ?? "?";
  const what = `${prettyText(type?.text).toLowerCase() || "a"} tone at ${prettyText(level?.text)} dB`;
  return (
    <section className="card section">
      <header className="section-head">
        <h3>Oscillator</h3>
        <button
          type="button"
          className={"btn big osc-btn" + (running ? " lit" : "")}
          aria-pressed={running}
          disabled={!can || on === undefined}
          onClick={() => (running ? store.set("/-stat/osc/on", 0) : setAsking(true))}
        >
          {running ? "■ Stop the tone" : "Start the tone…"}
        </button>
      </header>
      {running && (
        <p className="warn-box" role="status">
          The oscillator sends a {what} to {where}.
        </p>
      )}
      <div className="fields">
        <div className="field">
          <span className="field-label">Goes to</span>
          <select
            className="select"
            value={String(dest?.value ?? "")}
            disabled={!canDest || dest === undefined}
            onChange={(e) => store.set("/config/osc/dest", Number(e.target.value))}
          >
            {OSC_DESTS.map((n, i) => (
              <option key={n} value={i}>
                {n}
              </option>
            ))}
          </select>
        </div>
        <FieldRow addr="/config/osc/type" label="Type" />
        <FieldRow addr="/config/osc/level" label="Level" />
        <FieldRow addr="/config/osc/fsel" label="Frequency used" />
        <FieldRow addr="/config/osc/f1" label="F1" />
        <FieldRow addr="/config/osc/f2" label="F2" />
      </div>
      {asking && (
        <ConfirmDialog
          title={`Send a ${what} to ${where}?`}
          confirm="Start the tone"
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            store.set("/-stat/osc/on", 1);
          }}
        >
          <p>
            {/^(L|R|L\+R|M\/C)$/.test(where)
              ? "The main mix plays it: the audience hears it through the PA."
              : `What ${where} feeds plays it: wedges, in-ears, recordings.`}
          </p>
        </ConfirmDialog>
      )}
    </section>
  );
}
