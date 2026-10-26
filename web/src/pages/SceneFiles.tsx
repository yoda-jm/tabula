// The Scenes page's Scene files tab: the .scn files kept on the machine
// running tabula. A file is sent to the console (as the editors' PC →
// Mixer does), the console's settings are saved as one, and a file of this
// computer (GVO-2026.scn from M32-Edit, a USB stick…) is kept with them.

import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ConfirmDialog } from "../components/shell";
import { ApiError } from "../lib/api";
import { formatBytes } from "../lib/audio";
import type { SceneFile, SceneFiles as Listing } from "../lib/scenefiles";
import { headerOf, looksLikeScene, sceneFileName, sceneFilesApi } from "../lib/scenefiles";
import { useCanWrite, useSceneLoad, useStatus } from "../store/hooks";
import { store } from "../store/store";

interface Ask {
  title: string;
  confirm: string;
  body: ReactNode;
  go(): Promise<void>;
}

export function SceneFiles() {
  const [list, setList] = useState<Listing | null>(null);
  const [error, setError] = useState("");
  const [ask, setAsk] = useState<Ask | null>(null);
  const can = useCanWrite();
  const status = useStatus();
  const loading = useSceneLoad();
  const busy = !!loading && !loading.done;

  const refresh = useCallback(async () => {
    try {
      const l = await sceneFilesApi.list();
      setList(l);
      setError("");
      if (l.loading) store.setSceneLoad(l.loading);
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    void refresh();
  }, [refresh]);

  /** act runs a command, toasts how it went, and refreshes the list; it tells whether it worked. */
  const act = async (what: string, f: () => Promise<unknown>): Promise<boolean> => {
    setAsk(null);
    try {
      await f();
      store.toast(`${what}: done.`);
      return true;
    } catch (e) {
      const msg = e instanceof ApiError && e.status === 403 ? `${e.message}: arm writes first.` : (e as Error).message;
      store.toast(`${what}: ${msg}`, "error");
      return false;
    } finally {
      void refresh();
    }
  };

  const load = (f: SceneFile) =>
    setAsk({
      title: `Load ${f.name || f.file} into the console?`,
      confirm: "Load",
      body: (
        <>
          <p>
            <strong>Every parameter of the file changes at once on the console</strong> (faders, routing, effects, preamps,
            phantom power…), as M32-Edit's PC → Mixer does.
          </p>
          <p className="muted">
            The console's scene safes do not apply. The current mix is lost unless it is saved first (Save below, or as a scene
            of the console).
          </p>
        </>
      ),
      go: async () => {
        const p = await sceneFilesApi.load(f.file);
        store.setSceneLoad(p);
        if (p.error) throw new Error(p.error);
      },
    });

  return (
    <>
      <OpenFile files={list?.files ?? []} ask={setAsk} act={act} />
      <SaveFile files={list?.files ?? []} connected={status.connected} ask={setAsk} act={act} />
      <section className="card">
        <header className="section-head">
          <h3>
            Scene files <span className="muted">({list?.files.length ?? 0})</span>
          </h3>
          <button type="button" className="btn" onClick={() => void refresh()}>
            Refresh
          </button>
        </header>
        {list && (
          <p className="muted">
            In <code className="wrap">{list.dir}</code> on the machine running tabula (<code>-scenes</code>).
          </p>
        )}
        {error && <p className="error">{error}</p>}
        <LoadProgress />
        {list && !list.files.length && <p className="muted">No scene file yet: open one from this computer, or save the console's settings.</p>}
        <div className="slots">
          {list?.files.map((f) => (
            <div key={f.file} className="slot scene-file">
              <div className="slot-head">
                <span className="slot-name static">
                  <strong>{f.name || f.file}</strong>
                  {f.note && <span className="muted slot-note">{f.note}</span>}
                  <span className="muted slot-note">
                    {f.file} · {formatBytes(f.size)} · {new Date(f.modified).toLocaleString()}
                  </span>
                </span>
                <button type="button" className="btn primary" disabled={!can || busy} onClick={() => load(f)}>
                  {loading && !loading.done && loading.file === f.file ? "Loading…" : "Load"}
                </button>
                <a className="btn" href={sceneFilesApi.downloadUrl(f.file)} download={f.file} title="Download the file">
                  ⤓
                </a>
                <button
                  type="button"
                  className="btn danger"
                  aria-label={`Delete ${f.file}`}
                  title="Delete the file"
                  onClick={() =>
                    setAsk({
                      title: `Delete ${f.file}?`,
                      confirm: "Delete",
                      body: <p>The file is removed from the machine running tabula. The console keeps its settings.</p>,
                      go: () => sceneFilesApi.remove(f.file),
                    })
                  }
                >
                  ✕
                </button>
              </div>
            </div>
          ))}
        </div>
        {!can && list?.files.length ? <p className="muted lib-note">Loading a file changes the console: it needs armed writes.</p> : null}
      </section>
      {ask && (
        <ConfirmDialog title={ask.title} confirm={ask.confirm} onCancel={() => setAsk(null)} onConfirm={() => void act(ask.title.replace(/\?$/, ""), ask.go)}>
          {ask.body}
        </ConfirmDialog>
      )}
    </>
  );
}

/** LoadProgress shows the file being sent to the console, then how it went. */
function LoadProgress() {
  const p = useSceneLoad();
  if (!p) return null;
  const frac = p.total ? p.sent / p.total : 0;
  return (
    <div className={"scene-progress" + (p.error ? " failed" : "")} role="status">
      {!p.done ? (
        <p>
          Loading <strong>{p.file}</strong> into the console…
        </p>
      ) : p.error ? (
        <p className="error">
          {p.file}: {p.error}
        </p>
      ) : (
        <p>
          <strong>{p.file}</strong> loaded: {p.sent} lines.
        </p>
      )}
      {!p.done && (
        <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={p.total} aria-valuenow={p.sent}>
          <div className="progress-bar" style={{ width: `${frac * 100}%` }} />
          <span className="progress-text">
            {p.sent} / {p.total}
          </span>
        </div>
      )}
      {p.skipped?.length ? (
        <details>
          <summary className="note">
            {p.skipped.length} line{p.skipped.length === 1 ? "" : "s"} this console does not know, skipped (another model or
            firmware)
          </summary>
          <pre className="skipped">{p.skipped.join("\n")}</pre>
        </details>
      ) : null}
    </div>
  );
}

interface FormProps {
  files: SceneFile[];
  ask(a: Ask): void;
  act(what: string, f: () => Promise<unknown>): Promise<boolean>;
}

/** OpenFile keeps a scene file of this computer with the others. */
function OpenFile({ files, ask, act }: FormProps) {
  const input = useRef<HTMLInputElement>(null);
  const [error, setError] = useState("");
  const open = async (file: File) => {
    setError("");
    const text = await file.text();
    if (!looksLikeScene(text)) {
      setError(`${file.name} is not a scene file (#4.0# "name" … then /ch/01/config …).`);
      return;
    }
    const name = /\.scn$/i.test(file.name) ? file.name : sceneFileName(headerOf(text)?.name || file.name);
    const keep = (replace: boolean) => act(`Keep ${name}`, () => sceneFilesApi.keep(name, text, replace));
    if (files.some((f) => f.file === name)) {
      ask({
        title: `Replace ${name}?`,
        confirm: "Replace",
        body: <p>A scene file of this name is kept already: it is replaced by the one opened.</p>,
        go: () => sceneFilesApi.keep(name, text, true),
      });
    } else void keep(false);
  };
  return (
    <section className="card">
      <h3 className="save-title">Open a scene file</h3>
      <div className="row">
        <span className="grow muted">
          A .scn file of this computer (saved by the console on a USB stick, by M32-Edit or X32-Edit): it is kept with the
          others, then Load sends it to the console.
        </span>
        <input
          ref={input}
          type="file"
          accept=".scn,text/plain"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = ""; // the same file can be opened again
            if (f) void open(f);
          }}
        />
        <button type="button" className="btn" onClick={() => input.current?.click()}>
          Open a file…
        </button>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  );
}

/** SaveFile saves the console's settings as a scene file. */
function SaveFile({ files, connected, ask, act }: FormProps & { connected: boolean }) {
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const file = sceneFileName(name);
  const taken = !!name.trim() && files.some((f) => f.file === file);
  const save = (replace: boolean) =>
    sceneFilesApi.save(name.trim(), note.trim(), replace).then(() => {
      setName("");
      setNote("");
    });
  return (
    <section className="card">
      <h3 className="save-title">Save the console's settings as a file</h3>
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (!name.trim()) return;
          if (taken) {
            ask({
              title: `Replace ${file}?`,
              confirm: "Replace",
              body: <p>The file is replaced by the console's current settings.</p>,
              go: () => save(true),
            });
          } else void act(`Save ${file}`, () => save(false));
        }}
      >
        <label className="grow">
          <span className="field-label">Name</span>
          <input className="input" type="text" value={name} maxLength={31} placeholder="GVO-2026" onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="grow">
          <span className="field-label">Note (optional)</span>
          <input className="input" type="text" value={note} maxLength={31} onChange={(e) => setNote(e.target.value)} />
        </label>
        <button type="submit" className="btn" disabled={!connected || !name.trim()}>
          {taken ? "Replace" : "Save"}
        </button>
      </form>
      {name.trim() && <p className="muted">As {file}. Reading the console needs no armed writes.</p>}
    </section>
  );
}
