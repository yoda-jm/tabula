// Scenes, snippets and presets: what the console's library holds, with
// recall (after a confirmation), save, rename, copy and delete. Every
// command changes the console, so they all need armed writes. The Scene
// files tab holds the .scn files of the machine running tabula
// (SceneFiles.tsx).

import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { ConfirmDialog } from "../components/shell";
import { ApiError } from "../lib/api";
import type { KindInfo, LibKind, LibraryRequest, Slot } from "../lib/library";
import { KINDS, SCOPE_ALL, SCOPE_PARTS, describe, firstFree, libraryApi, readSlots, slotKey, slotLabel, slotPath } from "../lib/library";
import { usePersistent } from "../lib/route";
import { stripName } from "../lib/routing";
import { INDEXED_STRIPS } from "../lib/strips";
import { useCanWrite, useNodes, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";
import { NotConnected } from "./common";
import { SceneFiles } from "./SceneFiles";

/** The strips a channel preset is saved from or loaded to, by index 0-71 (DCAs excluded). */
const PRESET_STRIPS = INDEXED_STRIPS;

interface Ask {
  req: LibraryRequest;
  name: string;
  /** what the confirmation explains */
  body: ReactNode;
}

export function ScenesPage() {
  const [kind, setKind] = usePersistent<LibKind | "files">("tabula.library", "scene");
  const k = KINDS.find((i) => i.kind === kind) ?? KINDS[0];
  const [showEmpty, setShowEmpty] = usePersistent("tabula.library.empty", false);
  const paths = useMemo(() => Array.from({ length: 100 }, (_, i) => slotPath(k, i)), [k]);
  useNodes(paths);
  const can = useCanWrite();
  const [ask, setAsk] = useState<Ask | null>(null);
  const [busy, setBusy] = useState(false);
  const slots = readSlots(lookup, k);
  const used = slots.filter((s) => s.used);
  const shown = showEmpty ? slots : used;
  const current = useCurrent(k);

  /** run sends a command and reports it; it tells whether the console did it. */
  const run = async (req: LibraryRequest, name: string): Promise<boolean> => {
    setAsk(null);
    setBusy(true);
    try {
      await libraryApi.run(req);
      store.toast(`${describe(req, name)}: done.`);
      return true;
    } catch (e) {
      const msg = e instanceof ApiError && e.status === 403 ? `${e.message}: arm writes first.` : (e as Error).message;
      store.toast(`${describe(req, name)}: ${msg}`, "error");
      return false;
    } finally {
      setBusy(false);
    }
  };
  const actions: Actions = { can: can && !busy, ask: setAsk, run };

  return (
    <div className="page library">
      <div className="banks" role="tablist" aria-label="Library">
        {KINDS.map((i) => (
          <button
            key={i.kind}
            type="button"
            role="tab"
            aria-selected={i.kind === kind}
            className={"bank" + (i.kind === kind ? " active" : "")}
            onClick={() => setKind(i.kind)}
          >
            {i.label}
          </button>
        ))}
        <button
          type="button"
          role="tab"
          aria-selected={kind === "files"}
          className={"bank" + (kind === "files" ? " active" : "")}
          onClick={() => setKind("files")}
        >
          Scene files
        </button>
      </div>
      <NotConnected />
      {kind === "files" ? (
        <SceneFiles />
      ) : (
        <>
          {!can && (
            <p className="warn-box">
              Recalling, saving or changing the library changes the console: it needs armed writes (top right).
            </p>
          )}
          {/* keyed by kind, like the slots: a form's state belongs to its library */}
          <SaveForm key={k.kind} k={k} slots={slots} actions={actions} />
          <section className="card">
            <header className="section-head">
              <h3>
                {k.label} <span className="muted">({used.length} of 100)</span>
              </h3>
              <label className="check">
                <input type="checkbox" checked={showEmpty} onChange={(e) => setShowEmpty(e.target.checked)} /> Empty slots
              </label>
            </header>
            {!shown.length && <p className="muted">The console holds no {k.one}.</p>}
            <div className="slots">
              {shown.map((s) => (
                <SlotRow key={slotKey(k, s.slot)} k={k} s={s} slots={slots} current={s.slot === current} actions={actions} />
              ))}
            </div>
            {k.kind === "scene" && (
              <p className="muted lib-note">
                A recall loads the whole scene; the console applies its scene safes. A .scn file of this computer (saved
                by M32-Edit, or on a USB stick): the Scene files tab.
              </p>
            )}
          </section>
        </>
      )}
      {ask && (
        <ConfirmDialog
          title={describe(ask.req, ask.name) + "?"}
          confirm={ask.req.op === "load" ? "Recall" : "Go ahead"}
          onCancel={() => setAsk(null)}
          onConfirm={() => void run(ask.req, ask.name)}
        >
          {ask.body}
        </ConfirmDialog>
      )}
    </div>
  );
}

/** useCurrent is the scene or snippet the console's scenes page highlights, when it shows that kind. */
function useCurrent(k: KindInfo): number {
  const pos = useValue("/-show/prepos/current");
  const mode = useValue("/-prefs/show_control");
  const want = k.kind === "scene" ? "SCENES" : k.kind === "snippet" ? "SNIPPETS" : "";
  return want && mode?.text.trim() === want && pos ? Number(pos.value) : -1;
}

interface Actions {
  can: boolean;
  ask(a: Ask): void;
  run(req: LibraryRequest, name: string): Promise<boolean>;
}

function recallBody(k: KindInfo, target?: string): ReactNode {
  switch (k.kind) {
    case "scene":
      return (
        <>
          <p>
            <strong>Every parameter of the console changes at once</strong> (faders, routing, effects…), as on the console's
            scenes page.
          </p>
          <p className="muted">The current mix is lost unless it is saved first.</p>
        </>
      );
    case "snippet":
      return <p>The parameters stored in the snippet change at once on the console.</p>;
    case "libchan":
      return <p>The chosen sections of {target} change at once on the console.</p>;
    case "libfx":
      return <p>The effect and its settings replace what is in {target}.</p>;
    case "librout":
      return <p>The console's whole routing changes at once: inputs and outputs may move.</p>;
    default:
      return <p>The AES50/DP48 settings change at once.</p>;
  }
}

function SlotRow(props: { k: KindInfo; s: Slot; slots: Slot[]; current: boolean; actions: Actions }) {
  const { k, s, current, actions } = props;
  const [open, setOpen] = useState(false);
  const label = slotLabel(k, s.slot);
  const simpleLoad = !k.target;
  const recall = () => actions.ask({ req: { op: "load", kind: k.kind, index: s.slot }, name: s.name, body: recallBody(k) });
  return (
    <div className={"slot" + (s.used ? "" : " empty") + (current ? " current" : "") + (open ? " open" : "")}>
      <div className="slot-head">
        <code className="slot-num">{label}</code>
        <button type="button" className="slot-name" aria-expanded={open} onClick={() => setOpen(!open)}>
          <strong>{s.used ? s.name || "(no name)" : "empty"}</strong>
          {current && <span className="badge">current</span>}
          {s.note && <span className="muted slot-note">{s.note}</span>}
        </button>
        {s.used && simpleLoad && (
          <button type="button" className="btn primary" disabled={!actions.can} onClick={recall}>
            {k.kind === "scene" || k.kind === "snippet" ? "Recall" : "Load"}
          </button>
        )}
        {s.used && !simpleLoad && (
          <button type="button" className="btn primary" onClick={() => setOpen(true)}>
            Load…
          </button>
        )}
        <button
          type="button"
          className="btn slot-more"
          aria-label={`More for ${label}`}
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          {open ? "▴" : "▾"}
        </button>
      </div>
      {open && <SlotActions k={k} s={s} slots={props.slots} actions={actions} />}
    </div>
  );
}

function SlotActions({ k, s, slots, actions }: { k: KindInfo; s: Slot; slots: Slot[]; actions: Actions }) {
  const [name, setName] = useState(s.name);
  const [to, setTo] = useState("");
  const [target, setTarget] = useState(0);
  const [scope, setScope] = useState(SCOPE_ALL);
  const label = slotLabel(k, s.slot);
  const toSlot = k.first ? Number(to) - 1 : Number(to);
  const toValid = to.trim() !== "" && Number.isInteger(toSlot) && toSlot >= 0 && toSlot <= 99 && toSlot !== s.slot;
  const targetName = k.target === "strip" ? stripText(target) : `FX ${target + 1}`;
  return (
    <div className="slot-body">
      {s.used && k.target && (
        <div className="row slot-load">
          <label>
            <span className="field-label">Load to</span>
            {k.target === "strip" ? (
              <StripSelect value={target} onChange={setTarget} />
            ) : (
              <FxSelect value={target} onChange={setTarget} />
            )}
          </label>
          {k.kind === "libchan" && (
            <fieldset className="scope">
              <legend className="field-label">Sections</legend>
              {SCOPE_PARTS.map((p, bit) => (
                <label key={p} className="check">
                  <input
                    type="checkbox"
                    checked={(scope & (1 << bit)) !== 0}
                    onChange={(e) => setScope(e.target.checked ? scope | (1 << bit) : scope & ~(1 << bit))}
                  />{" "}
                  {p}
                </label>
              ))}
            </fieldset>
          )}
          <button
            type="button"
            className="btn primary"
            disabled={!actions.can || (k.kind === "libchan" && scope === 0)}
            onClick={() =>
              actions.ask({
                req: { op: "load", kind: k.kind, index: s.slot, target, ...(k.kind === "libchan" ? { scope } : {}) },
                name: s.name,
                body: recallBody(k, targetName),
              })
            }
          >
            Load to {targetName}
          </button>
        </div>
      )}
      {s.used && (
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim() && name.trim() !== s.name)
              void actions.run({ op: "rename", kind: k.kind, index: s.slot, name: name.trim() }, s.name);
          }}
        >
          <label className="grow">
            <span className="field-label">Name</span>
            <input className="input" type="text" value={name} maxLength={31} onChange={(e) => setName(e.target.value)} />
          </label>
          <button type="submit" className="btn" disabled={!actions.can || !name.trim() || name.trim() === s.name}>
            Rename
          </button>
        </form>
      )}
      <div className="row">
        {s.used && !k.noCopy && (
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              if (!toValid) return;
              const req: LibraryRequest = { op: "copy", kind: k.kind, index: s.slot, to: toSlot };
              const dest = slots[toSlot];
              if (dest?.used) {
                actions.ask({
                  req,
                  name: s.name,
                  body: (
                    <p>
                      Slot {slotLabel(k, toSlot)} holds “{dest.name}”: it is replaced by {k.one} {label}.
                    </p>
                  ),
                });
              } else void actions.run(req, s.name);
            }}
          >
            <label>
              <span className="field-label">Copy to slot</span>
              <input className="input num" type="number" inputMode="numeric" value={to} onChange={(e) => setTo(e.target.value)} />
            </label>
            <button type="submit" className="btn" disabled={!actions.can || !toValid}>
              Copy
            </button>
          </form>
        )}
        {s.used && (
          <button
            type="button"
            className="btn danger"
            disabled={!actions.can}
            onClick={() =>
              actions.ask({
                req: { op: "delete", kind: k.kind, index: s.slot },
                name: s.name,
                body: <p>The {k.one} is removed from the console's memory.</p>,
              })
            }
          >
            Delete {label}
          </button>
        )}
      </div>
      {!s.used && <p className="muted">Empty. Save the current settings here with the form above.</p>}
    </div>
  );
}

/** SaveForm saves the current settings in a slot (asking before overwriting one). */
function SaveForm({ k, slots, actions }: { k: KindInfo; slots: Slot[]; actions: Actions }) {
  const free = firstFree(slots);
  const [slot, setSlot] = useState<number | null>(null);
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [target, setTarget] = useState(0);
  const at = slot ?? (free < 0 ? 0 : free);
  const taken = slots[at]?.used;
  const save = () => {
    const req: LibraryRequest = {
      op: "save",
      kind: k.kind,
      index: at,
      name: name.trim(),
      ...(k.notes ? { note: note.trim() } : {}),
      ...(k.target ? { target } : {}),
    };
    if (taken) {
      actions.ask({
        req,
        name: slots[at].name,
        body: (
          <p>
            Slot {slotLabel(k, at)} holds “{slots[at].name}”: it is replaced by the current settings.
          </p>
        ),
      });
    } else {
      void actions.run(req, "").then((ok) => {
        if (!ok) return; // keep what was typed
        setName("");
        setNote("");
        setSlot(null);
      });
    }
  };
  const from = k.target === "strip" ? "a strip" : k.target === "fx" ? "an effect slot" : "the console";
  return (
    <section className="card">
      <h3 className="save-title">Save {k.one}</h3>
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) save();
        }}
      >
        <label>
          <span className="field-label">Slot</span>
          <select className="select" value={at} onChange={(e) => setSlot(Number(e.target.value))}>
            {slots.map((s) => (
              <option key={s.slot} value={s.slot}>
                {slotLabel(k, s.slot)} · {s.used ? s.name || "(no name)" : "empty"}
              </option>
            ))}
          </select>
        </label>
        {k.target && (
          <label>
            <span className="field-label">From {from}</span>
            {k.target === "strip" ? (
              <StripSelect value={target} onChange={setTarget} />
            ) : (
              <FxSelect value={target} onChange={setTarget} />
            )}
          </label>
        )}
        <label className="grow">
          <span className="field-label">Name</span>
          <input
            className="input"
            type="text"
            value={name}
            maxLength={31}
            placeholder="Name"
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        {k.notes && (
          <label className="grow">
            <span className="field-label">Note (optional)</span>
            <input className="input" type="text" value={note} maxLength={31} onChange={(e) => setNote(e.target.value)} />
          </label>
        )}
        <button type="submit" className="btn" disabled={!actions.can || !name.trim()}>
          {taken ? "Replace" : "Save"}
        </button>
      </form>
    </section>
  );
}

function stripText(i: number): string {
  const s = PRESET_STRIPS[i];
  if (!s) return String(i);
  const n = stripName(lookup, s.path);
  return n ? `${s.label} ${n}` : s.label;
}

function StripSelect({ value, onChange }: { value: number; onChange(v: number): void }) {
  useNodes(PRESET_STRIPS.map((s) => `${s.path}/config`));
  return (
    <select className="select" value={value} onChange={(e) => onChange(Number(e.target.value))}>
      {PRESET_STRIPS.map((s, i) => (
        <option key={s.path} value={i}>
          {stripText(i)}
        </option>
      ))}
    </select>
  );
}

function FxSelect({ value, onChange }: { value: number; onChange(v: number): void }) {
  return (
    <select className="select" value={value} onChange={(e) => onChange(Number(e.target.value))}>
      {Array.from({ length: 8 }, (_, i) => (
        <option key={i} value={i}>
          FX {i + 1} · {lookup.text(`/fx/${i + 1}/type`)?.trim() ?? ""}
        </option>
      ))}
    </select>
  );
}
