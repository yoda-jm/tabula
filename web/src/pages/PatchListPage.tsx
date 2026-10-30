// Patch list: a PDF of the inputs and outputs to hand to the venue's sound
// engineer. The rows come from the console's routing (lib/patchlist.ts);
// here the engineer ticks the ones to print and types a note for each (the
// mic, the stand), kept by tabula with the title and the boxes' names. The
// PDF is made in the browser (lib/pdf.ts): nothing to install.

import { useEffect, useMemo, useRef, useState } from "react";
import type { InputRow, OutputRow, PatchSettings } from "../lib/patchlist";
import {
  MAX_NOTE,
  WATERMARK,
  boxesOf,
  fileName,
  inputRows,
  listed,
  outputRows,
  patchlistApi,
  patchlistPdf,
  reportedBoxes,
} from "../lib/patchlist";
import { usePersistent } from "../lib/route";
import { colourOf } from "../lib/strips";
import { useNodes, useSchema, useStatus } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";
import { NotConnected } from "./common";

/** The nodes the rows read: re-render when any of them changes. */
const WATCHED =
  /^\/(config\/(routing|userrout|chlink|auxlink)|outputs\/|headamp\/|-stat\/aes50$|(ch|auxin|bus|mtx)\/\d\d\/config$|main\/(st|m)\/config$)/;

/** printedDate is when the PDF is made, as printed on it: "30 October 2026, 21:45". */
function printedDate(d: Date): string {
  const day = d.toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric" });
  return `${day}, ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

export function PatchListPage() {
  const [loaded, setLoaded] = useState<PatchSettings | null>(null);
  useEffect(() => {
    patchlistApi
      .get()
      .then(setLoaded)
      .catch((e: Error) => store.toast(`Patch list: ${e.message}`, "error"));
  }, []);
  if (!loaded) return <p className="banner">Loading the patch list…</p>;
  return <PatchListEditor initial={loaded} />;
}

/** PatchListEditor edits the settings tabula keeps, saving them a moment after each change. */
export function PatchListEditor({ initial }: { initial: PatchSettings }) {
  const schema = useSchema();
  const status = useStatus();
  const paths = useMemo(() => (schema ? schema.nodes.map((n) => n.path).filter((p) => WATCHED.test(p)) : []), [schema]);
  useNodes(paths);
  const [s, setS] = useState(initial);
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(""); // "Saved", or why not
  const [all, setAll] = usePersistent("tabula.patchlist.all", false);

  const pending = useRef<PatchSettings | null>(null); // changed, not saved yet

  // saved a moment after the last change, or when the page is left
  useEffect(() => {
    if (!dirty) return;
    const t = window.setTimeout(() => {
      const v = pending.current;
      pending.current = null;
      setDirty(false);
      if (!v) return;
      patchlistApi
        .save(v)
        .then(() => setSaved("Saved"))
        .catch((e: Error) => setSaved(`Not saved: ${e.message}`));
    }, 700);
    return () => window.clearTimeout(t);
  }, [s, dirty]);
  useEffect(
    () => () => {
      if (pending.current) patchlistApi.save(pending.current).catch(() => undefined);
    },
    [],
  );

  const edit = (change: Partial<PatchSettings>) => {
    const next = { ...s, ...change };
    pending.current = next;
    setS(next);
    setDirty(true);
    setSaved("");
  };
  const model = status.console?.info.model;
  const boxes = boxesOf(lookup, s);
  const inputs = inputRows(lookup, boxes);
  const outputs = outputRows(lookup, boxes, model, inputs);
  const nIn = inputs.filter((r) => listed(s, r)).length;
  const nOut = outputs.filter((r) => listed(s, r)).length;

  const toggle = (row: { key: string; auto: boolean }) => {
    const rows = { ...s.rows };
    const on = !listed(s, row);
    if (on === row.auto) delete rows[row.key];
    else rows[row.key] = on;
    edit({ rows });
  };
  const note = (key: string, text: string) => {
    const notes = { ...s.notes };
    if (text) notes[key] = text;
    else delete notes[key];
    edit({ notes });
  };
  const pdf = () => {
    const now = new Date();
    const bytes = patchlistPdf(s, inputs, outputs, boxes, { model, version: status.version, date: printedDate(now) }, now);
    return { url: URL.createObjectURL(new Blob([bytes], { type: "application/pdf" })), name: fileName(s.title, now) };
  };
  const download = () => {
    const { url, name } = pdf();
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
  };
  const open = () => {
    const { url } = pdf();
    window.open(url, "_blank");
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
  };
  return (
    <div className="page patchlist">
      <NotConnected />
      <section className="card">
        <header className="section-head">
          <h2>Patch list</h2>
          <span className="muted" role="status">
            {dirty ? "Saving…" : saved}
          </span>
        </header>
        <p className="muted">
          A PDF of the inputs and outputs to give to the venue's sound engineer. The rows come from the console's routing: tick
          the ones to print and add a note (the mic, the DI, the stand). tabula keeps the notes with the title.
        </p>
        <div className="pl-fields">
          <TextField id="pl-title" label="Title" value={s.title} placeholder="The band, the show" onChange={(title) => edit({ title })} />
          <TextField id="pl-details" label="Details" value={s.details} placeholder="The venue, the date" onChange={(details) => edit({ details })} />
          <TextField id="pl-contact" label="Contact" value={s.contact} placeholder="A name, a phone number" onChange={(contact) => edit({ contact })} />
          <TextField
            id="pl-box-a"
            label="Stage box on AES50 A"
            value={s.box_a}
            placeholder={reportedBoxes(lookup.text("/-stat/aes50/A")) || "DL32, S16… (none reported)"}
            onChange={(box_a) => edit({ box_a })}
          />
          <TextField
            id="pl-box-b"
            label="Stage box on AES50 B"
            value={s.box_b}
            placeholder={reportedBoxes(lookup.text("/-stat/aes50/B")) || "None reported"}
            onChange={(box_b) => edit({ box_b })}
          />
          <label className="pl-field" htmlFor="pl-paper">
            <span className="field-label">Paper</span>
            <select id="pl-paper" className="select" value={s.paper} onChange={(e) => edit({ paper: e.target.value as PatchSettings["paper"] })}>
              <option value="A4">A4</option>
              <option value="Letter">Letter</option>
            </select>
          </label>
        </div>
        <label className="check pl-mark">
          <input type="checkbox" id="pl-watermark" checked={s.watermark} onChange={(e) => edit({ watermark: e.target.checked })} />
          Print “{WATERMARK}” across each page
        </label>
        <div className="row pl-actions">
          <button type="button" className="btn primary" onClick={download}>
            Download the PDF
          </button>
          <button type="button" className="btn" onClick={open}>
            Open it
          </button>
          <span className="muted">
            {nIn} channel{nIn === 1 ? "" : "s"} and {nOut} output{nOut === 1 ? "" : "s"} to print
          </span>
        </div>
      </section>

      <section className="card">
        <header className="section-head">
          <h3>Inputs</h3>
          <ShowAll id="pl-all-in" all={all} hidden={inputs.length - nIn} onChange={setAll} />
        </header>
        <p className="muted pl-hint">
          Ticked by default: the named channels that come from a stage box or the console's own inputs, once per socket.
        </p>
        <div className="pl-table" role="table" aria-label="Inputs">
          <div className="pl-row pl-in head" role="row">
            <span role="columnheader" aria-label="Print" />
            <span role="columnheader">Ch</span>
            <span role="columnheader">Name</span>
            <span role="columnheader">Input</span>
            <span role="columnheader">48V</span>
            <span role="columnheader">Note</span>
          </div>
          {inputs
            .filter((r) => all || listed(s, r))
            .map((r) => (
              <InputLine key={r.key} row={r} on={listed(s, r)} note={s.notes[r.key] ?? ""} onToggle={() => toggle(r)} onNote={(t) => note(r.key, t)} />
            ))}
        </div>
      </section>

      <section className="card">
        <header className="section-head">
          <h3>Outputs</h3>
          <ShowAll id="pl-all-out" all={all} hidden={outputs.length - nOut} onChange={setAll} />
        </header>
        <p className="muted pl-hint">
          Ticked by default: the stage box outputs and the console's XLR outputs that carry something. Aux, P16 and AES/EBU
          outputs print when ticked.
        </p>
        <div className="pl-table" role="table" aria-label="Outputs">
          <div className="pl-row pl-out head" role="row">
            <span role="columnheader" aria-label="Print" />
            <span role="columnheader">Output</span>
            <span role="columnheader">Carries</span>
            <span role="columnheader">Note</span>
          </div>
          {outputs
            .filter((r) => all || listed(s, r))
            .map((r, i, shown) => (
              <OutputLine
                key={r.key}
                row={r}
                group={i === 0 || shown[i - 1].group !== r.group ? r.group : undefined}
                on={listed(s, r)}
                note={s.notes[r.key] ?? ""}
                onToggle={() => toggle(r)}
                onNote={(t) => note(r.key, t)}
              />
            ))}
        </div>
      </section>
    </div>
  );
}

/** ShowAll shows the rows left out of the PDF too (on both tables). */
function ShowAll({ id, all, hidden, onChange }: { id: string; all: boolean; hidden: number; onChange(v: boolean): void }) {
  return (
    <label className="check" htmlFor={id}>
      <input type="checkbox" id={id} checked={all} onChange={(e) => onChange(e.target.checked)} />
      Show the rows left out{hidden ? ` (${hidden})` : ""}
    </label>
  );
}

function TextField(props: { id: string; label: string; value: string; placeholder: string; onChange(v: string): void }) {
  return (
    <label className="pl-field" htmlFor={props.id}>
      <span className="field-label">{props.label}</span>
      <input
        id={props.id}
        className="input"
        value={props.value}
        maxLength={120}
        placeholder={props.placeholder}
        onChange={(e) => props.onChange(e.target.value)}
      />
    </label>
  );
}

function NoteField({ id, note, hints, onNote }: { id: string; note: string; hints: string[]; onNote(t: string): void }) {
  return (
    <span role="cell" className="pl-note">
      <input
        className="input"
        value={note}
        maxLength={MAX_NOTE}
        placeholder="Mic, DI, stand…"
        aria-label={`Note, ${id}`}
        onChange={(e) => onNote(e.target.value)}
      />
      {hints.length > 0 && <span className="muted pl-hints">{hints.join(" · ")}</span>}
    </span>
  );
}

function InputLine(props: { row: InputRow; on: boolean; note: string; onToggle(): void; onNote(t: string): void }) {
  const { row } = props;
  const c = colourOf(row.color);
  const label = row.key.startsWith("/ch/") ? `ch ${row.ch}` : row.ch.toLowerCase();
  return (
    <div className={"pl-row pl-in" + (props.on ? "" : " off")} role="row">
      <span role="cell">
        <input type="checkbox" checked={props.on} onChange={props.onToggle} aria-label={`Print ${label}`} />
      </span>
      <span role="cell" className="pl-ch">
        {row.ch}
      </span>
      <span role="cell" className="pl-name">
        {c.name !== "OFF" && <span className={`pl-swatch c-${c.name}`} aria-hidden="true" />}
        {row.name || <span className="muted">no name</span>}
      </span>
      <span role="cell" className={row.onStage ? "mono" : "mono muted"}>
        {row.socket}
      </span>
      <span role="cell">{row.phantom && <span className="badge phantom">48 V</span>}</span>
      <NoteField id={label} note={props.note} hints={row.hints} onNote={props.onNote} />
    </div>
  );
}

function OutputLine(props: { row: OutputRow; group?: string; on: boolean; note: string; onToggle(): void; onNote(t: string): void }) {
  const { row } = props;
  return (
    <>
      {props.group && (
        <div className="pl-group" role="row">
          <span role="cell">{props.group}</span>
        </div>
      )}
      <div className={"pl-row pl-out" + (props.on ? "" : " off")} role="row">
        <span role="cell">
          <input type="checkbox" checked={props.on} onChange={props.onToggle} aria-label={`Print ${row.socket}`} />
        </span>
        <span role="cell" className="pl-ch">
          {row.socket}
        </span>
        <span role="cell" className={row.carries === "OFF" ? "muted" : undefined}>
          {row.carries}
        </span>
        <NoteField id={row.socket} note={props.note} hints={row.hints} onNote={props.onNote} />
      </div>
    </>
  );
}
