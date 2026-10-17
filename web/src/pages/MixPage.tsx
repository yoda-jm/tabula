// A musician's in-ear mix, made for a phone on stage: a small desk of tall
// faders, one per source sending to their bus (channels, aux inputs, FX
// returns; a linked pair is one), with its name, colour and meter, their own
// bus volume fixed on the right, and an overview at the bottom to slide
// through the sources without touching a fader. A musician's access link
// opens this page only; the engineer sees any bus's mix as the musician does
// (#/mix/3).

import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode, RefObject } from "react";
import { Fader } from "../components/Fader";
import { StripMeter } from "../components/Meter";
import { Scribble } from "../components/Scribble";
import { Toasts } from "../components/shell";
import { api } from "../lib/api";
import type { MixSource } from "../lib/mix";
import { mixSources } from "../lib/mix";
import { go, usePersistent } from "../lib/route";
import type { Strip } from "../lib/strips";
import { ALL_STRIPS, colourOf, stripAddrs } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useAccess, useCanWrite, useLeaf, useNodes, useSchema, useStatus, useValue } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

const SOURCES = ALL_STRIPS.filter((s) => s.kind === "ch" || s.kind === "auxin" || s.kind === "fxrtn");
const BUSES = ALL_STRIPS.filter((s) => s.kind === "bus");
const LINK_NODES = ["/config/chlink", "/config/auxlink", "/config/fxlink", "/config/buslink"];
const CONFIG_NODES = SOURCES.map((s) => s.path + "/config");

/** MusicianApp is the whole GUI of a musician's access: their mix, nothing else. */
export function MusicianApp() {
  const who = useAccess();
  return (
    <div className="app iem-app">
      <main className="main">
        <MixPage bus={who?.bus} />
      </main>
      <Toasts />
    </div>
  );
}

/** Leave forgets a musician's access on this device. */
function Leave({ name }: { name?: string }) {
  const leave = async () => {
    await api.logout().catch(() => undefined);
    window.location.replace("/");
  };
  return (
    <span className="iem-leave">
      <span className="muted">{name ? `Access of ${name}` : "Access link"}</span>
      <button type="button" className="btn" onClick={leave} title="Forget this access on this device">
        Leave
      </button>
    </span>
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
const linked = (flag: string) => Number(store.getValue(flag)?.value) === 1;

function sourceInfo(strip: Strip) {
  const schema = store.schema;
  const a = schema ? stripAddrs(schema, strip) : undefined;
  return {
    name: prettyText(a?.name ? store.getValue(a.name)?.text : ""),
    colour: Number(a?.color ? store.getValue(a.color)?.value : 0) || 0,
  };
}

function BusMix({ bus }: { bus: number }) {
  const buses = useMixBuses(bus);
  const target = buses[0]; // writes go to the odd bus of a pair: the console mirrors them
  const stereo = buses.length === 2;
  const busPath = `/bus/${pad2(target)}`;
  const name = prettyText(useValue(`${busPath}/config/name`)?.text);
  const [showAll, setShowAll] = usePersistent("tabula.mix.all", false);
  const [pins, setPins] = usePersistent<string[]>(`tabula.mix.pins.${target}`, []);
  const [options, setOptions] = useState(false);
  const status = useStatus();
  const can = useCanWrite(`${busPath}/mix/fader`);
  const who = useAccess();
  const strips = useRef<HTMLDivElement>(null);
  const version = useNodes([...LINK_NODES, ...CONFIG_NODES]);

  const sources = useMemo(
    () => mixSources(SOURCES, linked, sourceInfo, { showAll, pins }),
    [showAll, pins, version], // version: links, names and colours
  );
  const togglePin = (p: string) => setPins(pins.includes(p) ? pins.filter((x) => x !== p) : [...pins, p]);

  return (
    <div className="iem">
      <div className="iem-bar">
        <h2>{name || `Bus ${target}`}</h2>
        <span className="muted">{stereo ? `Buses ${buses[0]}-${buses[1]}, stereo` : `Bus ${target}`}</span>
        <span className="topbar-gap" />
        <button type="button" className="btn" aria-expanded={options} onClick={() => setOptions(!options)}>
          Options
        </button>
      </div>
      {options && (
        <div className="iem-options">
          <label className="check">
            <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> Show unnamed sources
          </label>
          <WakeLockToggle />
          {who?.role === "musician" && <Leave name={who.name} />}
          <p className="muted">
            Drag a fader anywhere along it: it never jumps to your finger. Slide through the sources with the bar at the bottom. ★ keeps a source on the left.
          </p>
        </div>
      )}
      {status.connected && !can && <ReadOnlyNote role={who?.role} canWrite={who?.musicians_can_write} />}
      <div className="iem-desk">
        <div className="iem-strips" ref={strips}>
          {sources.map((s) => (
            <SourceStrip key={s.strip.path} source={s} bus={target} stereo={stereo} pinned={pins.includes(s.strip.path)} onPin={() => togglePin(s.strip.path)} />
          ))}
          {!sources.length && <p className="muted iem-empty">No named source sends to this mix: Options → Show unnamed sources.</p>}
        </div>
        <MasterStrip bus={target} stereo={stereo} />
      </div>
      <Overview sources={sources} bus={target} scroller={strips} />
    </div>
  );
}

function ReadOnlyNote({ role, canWrite }: { role?: string; canWrite?: boolean }) {
  let text = "Writes are not armed: arm them to change this mix.";
  if (role === "musician") {
    text = canWrite ? "This mix cannot be changed from here." : "The engineer has not let musicians change their mix for now.";
  }
  return <p className="banner">{text}</p>;
}

function SourceStrip(props: { source: MixSource; bus: number; stereo: boolean; pinned: boolean; onPin(): void }) {
  const { source, bus } = props;
  const { strip, partner } = source;
  const schema = useSchema();
  const send = `${strip.path}/mix/${pad2(bus)}`;
  const on = useValue(`${send}/on`);
  if (!schema || !store.leaf(`${send}/level`)) return null; // no send to this bus
  const a = stripAddrs(schema, strip);
  const off = on !== undefined && Number(on.value) === 0;
  const label = source.name || source.label;
  return (
    <div className={"iem-strip" + (off ? " off" : "") + (props.pinned ? " pinned" : "")}>
      <div className="iem-head">
        <Scribble strip={{ ...strip, label: source.label }} nameAddr={a.name} colorAddr={a.color} />
        <button
          type="button"
          className={"iem-pin" + (props.pinned ? " lit" : "")}
          aria-pressed={props.pinned}
          aria-label={`Keep ${label} on the left`}
          onClick={props.onPin}
        >
          ★
        </button>
      </div>
      {props.stereo &&
        (partner ? (
          <span className="iem-pan muted" title="A stereo source keeps the pans set on the console">
            stereo
          </span>
        ) : (
          <PanSlider addr={`${send}/pan`} label={label} />
        ))}
      <TallFader addr={`${send}/level`} label={`${label} in my mix`} meter={<StripMeter strip={strip} partner={partner} />} />
      <MuteButton addr={`${send}/on`} label={label} />
    </div>
  );
}

/** MasterStrip is the musician's own volume: their bus fader and mute. */
function MasterStrip({ bus, stereo }: { bus: number; stereo: boolean }) {
  const strip = BUSES[bus - 1];
  const busPath = `/bus/${pad2(bus)}`;
  return (
    <div className="iem-strip iem-master">
      <div className="iem-master-head">My volume</div>
      <TallFader addr={`${busPath}/mix/fader`} label="My volume" meter={<StripMeter strip={strip} partner={stereo ? BUSES[bus] : undefined} />} />
      <MuteButton addr={`${busPath}/mix/on`} label="my mix" />
    </div>
  );
}

/** TallFader is a meter and a fader filling the strip's height, with the level below. */
function TallFader({ addr, label, meter }: { addr: string; label: string; meter: ReactNode }) {
  const val = useValue(addr);
  const leaf = useLeaf(addr);
  const can = useCanWrite(addr);
  const text = prettyText(val?.text);
  return (
    <>
      <div className="iem-fader">
        {meter}
        <Fader
          value={Number(val?.value ?? 0)}
          steps={leaf?.field.type.steps}
          disabled={!can || val === undefined}
          label={label}
          valueText={`${text} dB`}
          onChange={(v) => store.set(addr, v)}
          onRelease={() => store.flush(addr)}
        />
      </div>
      <output className={"db" + (val?.pending ? " pending" : "")}>{val ? text : "—"}</output>
    </>
  );
}

function PanSlider({ addr, label }: { addr: string; label: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  if (!val) return <span className="iem-pan" />;
  const text = prettyText(val.text);
  const shown = text === "+0" || text === "0" ? "C" : text.startsWith("-") ? "L" + text.slice(1) : "R" + text.replace("+", "");
  return (
    <label className="iem-pan">
      <input
        type="range"
        min={0}
        max={1}
        step={0.01}
        value={Number(val.value)}
        disabled={!can}
        aria-label={`Pan of ${label} in my mix`}
        aria-valuetext={shown}
        onChange={(e) => store.set(addr, Number(e.target.value))}
        onPointerUp={() => store.flush(addr)}
        onDoubleClick={() => can && store.set(addr, 0.5)}
      />
      <span className="iem-pan-text">{shown}</span>
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
      MUTE
    </button>
  );
}

/**
 * Overview is the bar under the desk: every source as a block of its colour,
 * and a window on the ones in view. Dragging along it slides the desk, so the
 * faders are never touched by a swipe meant to scroll.
 */
function Overview({ sources, bus, scroller }: { sources: MixSource[]; bus: number; scroller: RefObject<HTMLDivElement> }) {
  const bar = useRef<HTMLDivElement>(null);
  const dragging = useRef<number | null>(null);
  const [view, setView] = useState({ left: 0, width: 1 });
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const update = () => setView({ left: el.scrollLeft / el.scrollWidth, width: el.clientWidth / el.scrollWidth });
    update();
    el.addEventListener("scroll", update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, [scroller, sources.length]);
  if (view.width >= 0.999) return null; // every source is in view
  const seek = (x: number) => {
    const el = scroller.current;
    const r = bar.current?.getBoundingClientRect();
    if (!el || !r) return;
    const f = (x - r.left) / r.width - el.clientWidth / el.scrollWidth / 2; // centre the window on the finger
    el.scrollLeft = f * el.scrollWidth;
  };
  return (
    <div
      ref={bar}
      className="iem-overview"
      aria-hidden // the desk scrolls by itself for the keyboard and screen readers
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        dragging.current = e.pointerId;
        seek(e.clientX);
      }}
      onPointerMove={(e) => dragging.current === e.pointerId && seek(e.clientX)}
      onPointerUp={() => (dragging.current = null)}
      onPointerCancel={() => (dragging.current = null)}
    >
      {sources.map((s) => (
        <OverviewCell key={s.strip.path} source={s} bus={bus} />
      ))}
      <div className="iem-window" style={{ left: `${view.left * 100}%`, width: `${view.width * 100}%` }} />
    </div>
  );
}

function OverviewCell({ source, bus }: { source: MixSource; bus: number }) {
  const on = useValue(`${source.strip.path}/mix/${pad2(bus)}/on`);
  const c = colourOf(source.colour);
  const off = on !== undefined && Number(on.value) === 0;
  return (
    <span className={`iem-cell c-${c.name}${off ? " off" : ""}`}>
      <span>{source.name || source.label}</span>
    </span>
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
