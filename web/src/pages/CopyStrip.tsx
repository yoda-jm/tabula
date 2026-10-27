// The Channel page's Copy to…: some sections of the strip shown (its EQ,
// dynamics, sends…) copied to other strips of its kind in one step, which
// Undo (top bar) takes back.

import { useEffect, useRef, useState } from "react";
import type { CopySection } from "../lib/api";
import { editApi } from "../lib/api";
import { COPY_SECTIONS, copySectionsOf, copyTargets } from "../lib/stripcopy";
import type { Strip } from "../lib/strips";
import { stripAddrs } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useCanWrite, useSchema } from "../store/hooks";
import { store } from "../store/store";

/** CopyStripButton opens the copy of a strip's sections, for strips that can be copied. */
export function CopyStripButton({ strip }: { strip: Strip }) {
  const schema = useSchema();
  const can = useCanWrite();
  const [open, setOpen] = useState(false);
  if (!schema || !copySectionsOf(schema, strip).length) return null;
  return (
    <>
      <button
        type="button"
        className="btn"
        disabled={!can}
        title={can ? `Copy sections of ${strip.label} to other strips` : "Copying changes the console: arm writes first"}
        onClick={() => setOpen(true)}
      >
        Copy to…
      </button>
      {open && <CopyStripDialog strip={strip} onClose={() => setOpen(false)} />}
    </>
  );
}

const DEFAULT: CopySection[] = ["gate", "dyn", "eq"];

function CopyStripDialog({ strip, onClose }: { strip: Strip; onClose(): void }) {
  const schema = useSchema();
  const offered = schema ? copySectionsOf(schema, strip) : [];
  const [sections, setSections] = useState<CopySection[]>(() => {
    const d = offered.filter((s) => DEFAULT.includes(s));
    return d.length ? d : offered.slice(0, 1);
  });
  const [to, setTo] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const cancel = useRef<HTMLButtonElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    cancel.current?.focus();
    const esc = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, []);
  if (!schema) return null;
  const targets = copyTargets(strip);
  const nameOf = (s: Strip) => prettyText(store.getValue(stripAddrs(schema, s).name ?? "")?.text);
  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  const label = (s: Strip) => [s.label, nameOf(s)].filter(Boolean).join(" ");
  const go = async () => {
    setBusy(true);
    try {
      await editApi.copyStrip(strip.path, to, sections);
      const what = COPY_SECTIONS.filter((c) => sections.includes(c.id)).map((c) => c.label.replace(/ \(.*/, ""));
      store.toast(`${what.join(", ")} of ${label(strip)} copied to ${to.length === 1 ? label(targets.find((t) => t.path === to[0])!) : `${to.length} strips`} (↶ Undo takes it back).`);
      onClose();
    } catch (e) {
      store.toast(`Copy: ${(e as Error).message}`, "error");
      setBusy(false);
    }
  };
  return (
    <div className="overlay" onClick={onClose}>
      <div className="dialog wide copy-dialog" role="dialog" aria-modal="true" aria-labelledby="copy-title" onClick={(e) => e.stopPropagation()}>
        <h2 id="copy-title">Copy {label(strip)} to…</h2>
        <div className="dialog-body">
          <fieldset className="copy-sections">
            <legend>What</legend>
            {COPY_SECTIONS.filter((c) => offered.includes(c.id)).map((c) => (
              <label key={c.id} className="check">
                <input type="checkbox" checked={sections.includes(c.id)} onChange={() => setSections(toggle(sections, c.id))} /> {c.label}
              </label>
            ))}
          </fieldset>
          <fieldset className="copy-targets">
            <legend>
              To{" "}
              <button type="button" className="btn small" onClick={() => setTo(to.length ? [] : targets.map((t) => t.path))}>
                {to.length ? "None" : "All"}
              </button>
            </legend>
            <div className="copy-grid">
              {targets.map((t) => (
                <label key={t.path} className={"copy-target" + (to.includes(t.path) ? " on" : "")}>
                  <input type="checkbox" checked={to.includes(t.path)} onChange={() => setTo(toggle(to, t.path))} />
                  <span>{t.label}</span>
                  <span className="muted">{nameOf(t)}</span>
                </label>
              ))}
            </div>
          </fieldset>
          <p className="muted">The strips chosen take these settings at once on the console; ↶ Undo (top bar) puts them back.</p>
        </div>
        <div className="dialog-actions">
          <button ref={cancel} type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn danger" disabled={busy || !to.length || !sections.length} onClick={() => void go()}>
            {to.length ? `Copy to ${to.length} strip${to.length === 1 ? "" : "s"}` : "Copy"}
          </button>
        </div>
      </div>
    </div>
  );
}
