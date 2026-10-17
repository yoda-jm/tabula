// A musician's in-ear mix, made for a phone on stage: their bus volume and
// mute, then one send slider per source (channels, aux inputs, FX returns).
// A musician's access link opens this page only; the engineer sees any bus's
// mix as the musician does (#/mix/3).

import { useEffect, useMemo, useRef, useState } from "react";
import { Scribble } from "../components/Scribble";
import { Toasts } from "../components/shell";
import { api } from "../lib/api";
import { meterFraction, toDbfs } from "../lib/meters";
import { go, usePersistent } from "../lib/route";
import type { Strip } from "../lib/strips";
import { ALL_STRIPS, stripAddrs } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useAccess, useCanWrite, useLeaf, useMeters, useSchema, useStatus, useValue } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

const SOURCES = ALL_STRIPS.filter((s) => s.kind === "ch" || s.kind === "auxin" || s.kind === "fxrtn");
const BUSES = ALL_STRIPS.filter((s) => s.kind === "bus");

/** MusicianApp is the whole GUI of a musician's access. */
export function MusicianApp() {
  const who = useAccess();
  const leave = async () => {
    await api.logout().catch(() => undefined);
    window.location.replace("/");
  };
  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">tabula</span>
        <span className="mix-who">{who?.name}</span>
        <span className="topbar-gap" />
        <button type="button" className="btn" onClick={leave} title="Forget this access on this device">
          Leave
        </button>
      </header>
      <main className="main">
        <MixPage bus={who?.bus} />
      </main>
      <Toasts />
    </div>
  );
}

/** NoAccess is shown to a device without an access link. */
export function NoAccess() {
  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">tabula</span>
      </header>
      <main className="main">
        <div className="page">
          <div className="card noaccess">
            <h2>No access</h2>
            <p>This device cannot see the console. Ask the engineer for an access link: a QR code to scan, or a link to open here.</p>
          </div>
        </div>
      </main>
    </div>
  );
}

export function MixPage({ bus }: { bus?: number }) {
  const schema = useSchema();
  const who = useAccess();
  const engineer = who?.role === "engineer";
  const n = bus && bus >= 1 && bus <= 16 ? bus : undefined;
  if (!schema) return <p className="banner">Loading…</p>;
  return (
    <div className="page mix">
      {engineer && <BusPicker bus={n} />}
      <NotConnected />
      {n ? <BusMix bus={n} /> : engineer ? <p className="muted">Pick the bus of an in-ear mix to see it as its musician does.</p> : null}
    </div>
  );
}

function BusPicker({ bus }: { bus?: number }) {
  return (
    <label className="mix-picker">
      <span>In-ear mix</span>
      <select className="select" value={bus ?? ""} onChange={(e) => go({ page: "mix", bus: Number(e.target.value) || undefined })}>
        <option value="">—</option>
        {BUSES.map((b, i) => (
          <BusOption key={b.path} strip={b} n={i + 1} />
        ))}
      </select>
    </label>
  );
}

function BusOption({ strip, n }: { strip: Strip; n: number }) {
  const name = prettyText(useValue(`${strip.path}/config/name`)?.text);
  return <option value={n}>{`Bus ${n}${name ? " — " + name : ""}`}</option>;
}

/** The buses of the mix: the bus, or the linked pair it belongs to (stereo). */
function useMixBuses(bus: number): number[] {
  const odd = bus - ((bus + 1) % 2);
  const link = useValue(`/config/buslink/${odd}-${odd + 1}`);
  return Number(link?.value) === 1 ? [odd, odd + 1] : [bus];
}

const pad2 = (n: number) => String(n).padStart(2, "0");

function BusMix({ bus }: { bus: number }) {
  const buses = useMixBuses(bus);
  const target = buses[0]; // writes go to the odd bus of a pair: the console mirrors them
  const stereo = buses.length === 2;
  const busPath = `/bus/${pad2(target)}`;
  const name = prettyText(useValue(`${busPath}/config/name`)?.text);
  const [showAll, setShowAll] = usePersistent("tabula.mix.all", false);
  const [pins, setPins] = usePersistent<string[]>(`tabula.mix.pins.${target}`, []);
  const status = useStatus();
  const can = useCanWrite(`${busPath}/mix/fader`);
  const who = useAccess();

  const sources = useMemo(() => {
    const pinned = SOURCES.filter((s) => pins.includes(s.path));
    return [...pinned, ...SOURCES.filter((s) => !pins.includes(s.path))];
  }, [pins]);
  const togglePin = (p: string) => setPins(pins.includes(p) ? pins.filter((x) => x !== p) : [...pins, p]);

  return (
    <>
      <section className="card mix-master">
        <div className="mix-title">
          <h2>{name || `Bus ${target}`}</h2>
          <span className="muted">{stereo ? `Buses ${buses[0]}-${buses[1]}, stereo` : `Bus ${target}`}</span>
        </div>
        <LevelBar group="0" index={47 + target} />
        <span className="mix-label">My volume</span>
        <div className="mix-row">
          <SendSlider addr={`${busPath}/mix/fader`} label="Mix volume" />
          <MuteButton addr={`${busPath}/mix/on`} label="Mix" />
        </div>
        {status.connected && !can && <ReadOnlyNote role={who?.role} canWrite={who?.musicians_can_write} />}
        <div className="mix-tools">
          <label className="check">
            <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> Show unnamed sources
          </label>
          <WakeLockToggle />
        </div>
      </section>
      <div className="mix-sources">
        {sources.map((s) => (
          <SourceRow
            key={s.path}
            strip={s}
            bus={target}
            stereo={stereo}
            showAll={showAll}
            pinned={pins.includes(s.path)}
            onPin={() => togglePin(s.path)}
          />
        ))}
      </div>
    </>
  );
}

function ReadOnlyNote({ role, canWrite }: { role?: string; canWrite?: boolean }) {
  let text = "Writes are not armed: arm them to change this mix.";
  if (role === "musician") {
    text = canWrite ? "This mix cannot be changed from here." : "The engineer has not let musicians change their mix for now.";
  }
  return <p className="banner">{text}</p>;
}

function SourceRow(props: { strip: Strip; bus: number; stereo: boolean; showAll: boolean; pinned: boolean; onPin(): void }) {
  const { strip, bus } = props;
  const schema = useSchema();
  const a = schema ? stripAddrs(schema, strip) : undefined;
  const name = prettyText(useValue(a?.name)?.text);
  const send = `${strip.path}/mix/${pad2(bus)}`;
  const sendOn = useValue(`${send}/on`);
  const level = useValue(`${send}/level`);
  if (!a || !level) return null; // no send to this bus (or not loaded yet)
  const used = name !== "" && name !== "-";
  if (!props.showAll && !used && !props.pinned) return null;
  const n = Number(strip.path.slice(strip.path.lastIndexOf("/") + 1));
  const meterIndex = strip.kind === "ch" ? n - 1 : strip.kind === "auxin" ? 31 + n : 39 + n;
  const off = sendOn !== undefined && Number(sendOn.value) === 0;
  return (
    <div className={"mix-source" + (off ? " off" : "")}>
      <div className="mix-source-head">
        <Scribble strip={strip} nameAddr={a.name} colorAddr={a.color} />
        <LevelBar group="0" index={meterIndex} />
        <button
          type="button"
          className={"btn pin" + (props.pinned ? " lit" : "")}
          aria-pressed={props.pinned}
          title={props.pinned ? "Unpin" : "Pin to the top"}
          onClick={props.onPin}
        >
          ★
        </button>
      </div>
      <div className="mix-row">
        <SendSlider addr={`${send}/level`} label={`${name || strip.label} in my mix`} />
        <MuteButton addr={`${send}/on`} label={name || strip.label} />
      </div>
      {props.stereo && <PanSlider addr={`${send}/pan`} />}
    </div>
  );
}

/** SendSlider is a wide horizontal slider on the console's level law. */
function SendSlider({ addr, label }: { addr: string; label: string }) {
  const val = useValue(addr);
  const leaf = useLeaf(addr);
  const can = useCanWrite(addr);
  const steps = leaf?.field.type.steps ?? 161;
  const text = prettyText(val?.text);
  return (
    <label className="mix-slider">
      <input
        type="range"
        min={0}
        max={1}
        step={1 / (steps - 1)}
        value={Number(val?.value ?? 0)}
        disabled={!can || val === undefined}
        aria-label={label}
        aria-valuetext={`${text} dB`}
        onChange={(e) => store.set(addr, Number(e.target.value))}
        onPointerUp={() => store.flush(addr)}
        onKeyUp={() => store.flush(addr)}
      />
      <output className={"db" + (val?.pending ? " pending" : "")}>{val ? text : "—"}</output>
    </label>
  );
}

function PanSlider({ addr }: { addr: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  if (!val) return null;
  const text = prettyText(val.text);
  const shown = text === "+0" || text === "0" ? "C" : text.startsWith("-") ? "L" + text.slice(1) : "R" + text.replace("+", "");
  return (
    <label className="mix-slider mix-pan">
      <span className="muted">Pan</span>
      <input
        type="range"
        min={0}
        max={1}
        step={0.01}
        value={Number(val.value)}
        disabled={!can}
        aria-label="Pan in my mix"
        aria-valuetext={shown}
        onChange={(e) => store.set(addr, Number(e.target.value))}
        onPointerUp={() => store.flush(addr)}
        onDoubleClick={() => can && store.set(addr, 0.5)}
      />
      <output className="db">{shown}</output>
    </label>
  );
}

function MuteButton({ addr, label }: { addr: string; label: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  const muted = val !== undefined && Number(val.value) === 0;
  return (
    <button
      type="button"
      className={"btn mute" + (muted ? " lit" : "")}
      aria-pressed={muted}
      aria-label={`Mute ${label}`}
      disabled={!can || val === undefined}
      onClick={() => store.set(addr, muted ? 1 : 0)}
    >
      {muted ? "OFF" : "ON"}
    </button>
  );
}

/** LevelBar is a small horizontal level meter from a meter group. */
function LevelBar({ group, index }: { group: string; index: number }) {
  const values = useMeters(group);
  const level = values?.[index];
  const f = level === undefined ? 0 : meterFraction(toDbfs(level));
  return (
    <div className="levelbar" aria-hidden>
      <div className="levelbar-fill" style={{ width: `${f * 100}%` }} />
    </div>
  );
}

/** WakeLockToggle keeps the phone's screen on while the mix page is open. */
function WakeLockToggle() {
  const supported = typeof navigator !== "undefined" && "wakeLock" in navigator;
  const [on, setOn] = usePersistent("tabula.mix.awake", false);
  const [active, setActive] = useState(false);
  const lock = useRef<WakeLockSentinel | null>(null);
  useEffect(() => {
    if (!supported || !on) return;
    let cancelled = false;
    const request = () =>
      navigator.wakeLock.request("screen").then(
        (l) => {
          if (cancelled) {
            void l.release();
            return;
          }
          lock.current = l;
          setActive(true);
          l.addEventListener("release", () => setActive(false));
        },
        () => setActive(false),
      );
    void request();
    // the lock is released when the page is hidden: take it again on return
    const onVisible = () => document.visibilityState === "visible" && void request();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", onVisible);
      void lock.current?.release();
      lock.current = null;
    };
  }, [supported, on]);
  if (!supported) return null;
  return (
    <label className="check" title={on && !active ? "The browser refused (the page may need HTTPS or a user gesture)" : undefined}>
      <input type="checkbox" checked={on} onChange={(e) => setOn(e.target.checked)} /> Keep the screen on
    </label>
  );
}
