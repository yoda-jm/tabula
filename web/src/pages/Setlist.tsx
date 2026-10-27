// The Record page's setlist: the show's songs, typed before it, one per
// line. While recording, one tap on ⚑ in the top bar (or M) marks the next
// song with its name; here it can be moved back or on.

import { useEffect, useState } from "react";
import { nextSong, parseSetlist, setlistApi, songLabel } from "../lib/setlist";
import { useSetlist } from "../store/hooks";
import { store } from "../store/store";

export function SetlistCard() {
  const setlist = useSetlist();
  const saved = (setlist?.songs ?? []).join("\n");
  const [text, setText] = useState<string | null>(null); // being typed; null: the saved one
  const [busy, setBusy] = useState(false);
  useEffect(() => setText(null), [saved]); // saved here or elsewhere: show it
  if (!setlist) return null;
  const shown = text ?? saved;
  const songs = parseSetlist(shown);
  const changed = songs.join("\n") !== saved;
  const next = nextSong(setlist);
  const act = async (f: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await f();
    } catch (e) {
      store.toast(`Setlist: ${(e as Error).message}`, "error");
    } finally {
      setBusy(false);
    }
  };
  const save = (from?: number) =>
    act(async () => {
      store.setSetlist(await setlistApi.save(songs, from));
      setText(null);
    });
  const moveTo = (n: number) => act(async () => store.setSetlist(await setlistApi.moveTo(n)));
  return (
    <section className="card setlist">
      <header className="section-head">
        <h2>Setlist</h2>
        <span className="muted">{songs.length ? `${songs.length} song${songs.length === 1 ? "" : "s"}` : "no song yet"}</span>
      </header>
      <p className="muted">
        One song per line, in the order of the show. While recording, ⚑ in the top bar (or the M key, on any page) marks the
        next song with its name: the songs are then found at once in the recording and its exports.
      </p>
      <textarea
        className="input setlist-text"
        rows={Math.min(14, Math.max(5, songs.length + 1))}
        value={shown}
        placeholder={"Opening\nSecond song\nThird song…"}
        spellCheck={false}
        aria-label="Songs, one per line"
        onChange={(e) => setText(e.target.value)}
      />
      <div className="row">
        <button type="button" className="btn primary" disabled={busy || !changed} onClick={() => void save()}>
          Save the setlist
        </button>
        {changed && (
          <button type="button" className="btn" disabled={busy} onClick={() => setText(null)}>
            Cancel
          </button>
        )}
        {changed && saved && (
          <button type="button" className="btn" disabled={busy} onClick={() => void save(0)}>
            Save, and start from the first song
          </button>
        )}
      </div>
      {setlist.songs.length > 0 && (
        <div className="row setlist-next">
          <span>
            Next to mark: <strong>{next ?? "all the songs are marked"}</strong>
          </span>
          <button type="button" className="btn small" disabled={busy || setlist.next === 0} onClick={() => void moveTo(setlist.next - 1)}>
            ◀ Back one
          </button>
          <button type="button" className="btn small" disabled={busy || !next} onClick={() => void moveTo(setlist.next + 1)}>
            Skip one ▶
          </button>
          {setlist.next > 0 && (
            <button type="button" className="btn small" disabled={busy} onClick={() => void moveTo(0)} title={`Next: ${songLabel(0, setlist.songs[0])}`}>
              From the first
            </button>
          )}
        </div>
      )}
    </section>
  );
}
