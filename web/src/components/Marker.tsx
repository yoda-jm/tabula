// The top bar's marker button, on every engineer page while recording: one
// tap (or the M key, outside a text field) marks the next song of the
// setlist with its name, or a timed marker when there is none left; ▾ marks
// another song, goes back one, or marks with a label of yours.

import { useEffect, useRef, useState } from "react";
import { addsMarker, formatDuration } from "../lib/session";
import type { Marked } from "../lib/setlist";
import { nextSong, setlistApi, songLabel } from "../lib/setlist";
import { useRecord, useSetlist } from "../store/hooks";
import { store } from "../store/store";

/** placeMarker adds a marker and says so; false when it failed (told too). */
export async function placeMarker(f: () => Promise<Marked>): Promise<boolean> {
  try {
    const m = await f();
    store.setRecord(m);
    store.toast(`Marked ${m.marked.label} at ${formatDuration(m.marked.t)}`);
    return true;
  } catch (e) {
    store.toast(`Marker: ${(e as Error).message}`, "error");
    return false;
  }
}

export function MarkerButton() {
  const rec = useRecord();
  const setlist = useSetlist();
  const [open, setOpen] = useState(false);
  const [label, setLabel] = useState("");
  const menu = useRef<HTMLDivElement>(null);
  const recording = !!rec?.recording;
  const next = nextSong(setlist);

  // M marks the next song, on every page (the field being typed in keeps its M)
  useEffect(() => {
    if (!recording) return;
    const onKey = (e: KeyboardEvent) => {
      if (!addsMarker(e)) return; // M held down: one marker, not one per key repeat
      if ((e.target as HTMLElement).closest?.("input, textarea, select, [contenteditable]")) return;
      e.preventDefault();
      void placeMarker(setlistApi.markNext);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [recording]);

  // the menu closes on a click elsewhere, or Escape
  useEffect(() => {
    if (!open) return;
    const away = (e: Event) => menu.current && !menu.current.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("pointerdown", away);
    window.addEventListener("keydown", esc);
    return () => {
      window.removeEventListener("pointerdown", away);
      window.removeEventListener("keydown", esc);
    };
  }, [open]);
  useEffect(() => {
    if (!recording) setOpen(false);
  }, [recording]);

  if (!recording) return null;
  const run = (f: () => Promise<Marked>) => {
    setOpen(false);
    void placeMarker(f);
  };
  const songs = setlist?.songs ?? [];
  return (
    <div className="marker" ref={menu}>
      <button
        type="button"
        className="marker-main"
        onClick={() => run(setlistApi.markNext)}
        title={next ? `Mark ${next} (M)` : "Add a marker (M)"}
        aria-label={next ? `Mark ${next}` : "Add a marker"}
      >
        <span aria-hidden>⚑</span>
        {next ? (
          <>
            <span className="marker-num">{setlist!.next + 1}</span>
            <span className="marker-song">{setlist!.songs[setlist!.next]}</span>
          </>
        ) : (
          <span className="marker-song">Marker</span>
        )}
      </button>
      <button type="button" className="marker-more" aria-expanded={open} aria-label="More markers" onClick={() => setOpen(!open)}>
        ▾
      </button>
      {open && (
        <div className="marker-menu" role="menu">
          {songs.length > 0 && (
            <>
              <p className="marker-head">Mark a song</p>
              <ol className="marker-songs">
                {songs.map((s, i) => (
                  <li key={i}>
                    <button
                      type="button"
                      role="menuitem"
                      className={i === setlist!.next ? "next" : i < setlist!.next ? "done" : ""}
                      onClick={() => run(() => setlistApi.markSong(i))}
                    >
                      {songLabel(i, s)}
                    </button>
                  </li>
                ))}
              </ol>
              <div className="marker-row">
                <button
                  type="button"
                  className="btn small"
                  disabled={!setlist || setlist.next === 0}
                  onClick={() => setlistApi.moveTo(setlist!.next - 1).then(store.setSetlist.bind(store), (e: Error) => store.toast(e.message, "error"))}
                >
                  ◀ Back one song
                </button>
                <button
                  type="button"
                  className="btn small"
                  disabled={!next}
                  onClick={() => setlistApi.moveTo(setlist!.next + 1).then(store.setSetlist.bind(store), (e: Error) => store.toast(e.message, "error"))}
                >
                  Skip one ▶
                </button>
              </div>
            </>
          )}
          <form
            className="marker-row"
            onSubmit={(e) => {
              e.preventDefault();
              run(() => setlistApi.markLabel(label.trim()));
              setLabel("");
            }}
          >
            <input
              className="input"
              type="text"
              value={label}
              placeholder="A label (Break, Encore…)"
              aria-label="Marker label"
              maxLength={80}
              onChange={(e) => setLabel(e.target.value)}
            />
            <button type="submit" className="btn small">
              {label.trim() ? "Mark" : "Timed marker"}
            </button>
          </form>
        </div>
      )}
    </div>
  );
}
