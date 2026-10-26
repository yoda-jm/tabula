// The USB stick in the console, as on its USB recorder screen: the player
// (the file playing, its time, play, pause, stop, previous and next) and
// the current directory of the stick, to open a folder, play a WAV file or
// load a scene from it. Commands change the console: armed writes.

import { useCallback, useEffect, useRef, useState } from "react";
import { ConfirmDialog } from "../components/shell";
import { ApiError } from "../lib/api";
import type { UsbEntry, UsbOp, UsbView } from "../lib/usb";
import { KIND_LABEL, TAPE_STATES, entryAction, formatClock, isPlaying, recordsToStick, usbApi } from "../lib/usb";
import { useCanWrite, useRecord, useStatus, useValue } from "../store/hooks";
import { store } from "../store/store";
import { NotConnected } from "./common";

export function UsbPage() {
  const status = useStatus();
  const can = useCanWrite();
  const mounted = useValue("/-stat/usbmounted");
  const [view, setView] = useState<UsbView | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [ask, setAsk] = useState<UsbEntry | null>(null);
  const latest = useRef(0); // a listing answered late does not replace a newer one

  const run = useCallback(async (f: () => Promise<UsbView | void>) => {
    const n = ++latest.current;
    setBusy(true);
    try {
      const v = await f();
      if (v && n === latest.current) setView(v);
      setError("");
    } catch (e) {
      const msg = e instanceof ApiError && e.status === 403 ? `${e.message}: arm writes first.` : (e as Error).message;
      setError(msg);
    } finally {
      setBusy(false);
    }
  }, []);
  const refresh = useCallback(() => run(usbApi.get), [run]);
  // read the stick again when tabula connects, and when one is plugged in or out
  useEffect(() => {
    if (status.connected) void refresh();
  }, [status.connected, mounted?.value, refresh]);

  const open = (e: UsbEntry) => {
    if (entryAction(e.kind)?.asks) setAsk(e);
    else void run(() => usbApi.open(e.index));
  };
  const op = (o: UsbOp) => void run(() => usbApi.op(o));

  return (
    <div className="page usb">
      <NotConnected />
      {!can && status.connected && (
        <p className="warn-box">Playing, opening a folder or loading a file changes the console: it needs armed writes (top right).</p>
      )}
      <Player can={can && !busy} onOp={op} />
      <section className="card">
        <header className="section-head">
          <h3>
            USB stick{view?.mounted && <span className="muted"> › {view.path || "root"}</span>}
          </h3>
          <button type="button" className="btn" disabled={busy || !status.connected} onClick={() => void refresh()}>
            Refresh
          </button>
        </header>
        {error && <p className="error">{error}</p>}
        {view && !view.mounted && <p className="muted">No USB stick in the console.</p>}
        {view?.mounted && !view.entries.length && <p className="muted">This folder is empty.</p>}
        <UsbList view={view} can={can && !busy} onOpen={open} />
        <p className="muted lib-note">
          The console plays the WAV files of its stick on its USB player; where they are heard is the console's routing of the
          player.
        </p>
      </section>
      {ask && (
        <ConfirmDialog
          title={`Load ${ask.name}?`}
          confirm="Load"
          onCancel={() => setAsk(null)}
          onConfirm={() => {
            const e = ask;
            setAsk(null);
            void run(async () => {
              const v = await usbApi.open(e.index);
              store.toast(`${e.name}: loaded by the console.`);
              return v;
            });
          }}
        >
          <p>
            <strong>The console loads the file from its USB stick</strong>: the parameters it holds change at once (a scene: the
            whole mix).
          </p>
          <p className="muted">The current mix is lost unless it is saved first.</p>
        </ConfirmDialog>
      )}
    </div>
  );
}

function UsbList({ view, can, onOpen }: { view: UsbView | null; can: boolean; onOpen(e: UsbEntry): void }) {
  const file = useValue("/-stat/tape/file");
  if (!view?.mounted) return null;
  return (
    <div className="slots">
      {view.entries.map((e) => {
        const act = entryAction(e.kind);
        const playing = e.kind === "wav" && isPlaying(String(file?.value ?? ""), view.path, e.name);
        return (
          <div key={e.index} className={"slot usb-entry" + (playing ? " current" : "")}>
            <div className="slot-head">
              <code className="slot-num">{e.kind === "parent" ? "↰" : String(e.index).padStart(3, "0")}</code>
              <span className="slot-name static">
                <strong>{e.kind === "parent" ? "Parent folder" : e.name}</strong>
                <span className="muted slot-note">{KIND_LABEL[e.kind]}</span>
                {playing && <span className="badge">in the player</span>}
              </span>
              {act && (
                <button type="button" className={"btn" + (e.kind === "wav" ? " primary" : "")} disabled={!can} onClick={() => onOpen(e)}>
                  {act.label}
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/** Player is the console's USB player: what it plays and its buttons. */
function Player({ can, onOp }: { can: boolean; onOp(o: UsbOp): void }) {
  const state = useValue("/-stat/tape/state");
  const file = useValue("/-stat/tape/file");
  const etime = useValue("/-stat/tape/etime");
  const rtime = useValue("/-stat/tape/rtime");
  const autoplay = useValue("/config/tape/autoplay");
  const [stopping, setStopping] = useState(false); // asking before stopping the console's own recording
  const backup = useRecord()?.usb;
  const st = String(state?.text ?? "STOP").trim();
  const recording = recordsToStick(st);
  const elapsed = Number(etime?.value ?? 0);
  const left = recording ? 0 : Math.max(0, Number(rtime?.value ?? 0));
  const total = elapsed + left;
  const path = String(file?.value ?? "");
  const name = path.split("/").filter(Boolean).pop() ?? "";
  const playing = st === "PLAY";
  // tabula's backup of the recording (Record page), not one started on the console
  const ours = !!backup?.file && backup.file === path && !backup.already;
  return (
    <section className="card player">
      <header className="section-head">
        <h3>USB player</h3>
        <span className={playing ? "danger-text" : "muted"}>{playing ? "▶ " : ""}{TAPE_STATES[st] ?? st}</span>
      </header>
      <p className="player-file">
        {name ? <strong>{name.replace(/\.wav$/i, "")}</strong> : <span className="muted">Nothing loaded: play a WAV file of the stick below.</span>}
        {path && <span className="muted mono"> {path}</span>}
      </p>
      <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={total} aria-valuenow={elapsed}>
        <div className="progress-bar" style={{ width: `${total ? (100 * elapsed) / total : 0}%` }} />
        <span className="progress-text">
          {formatClock(elapsed)}
          {!recording && ` / ${formatClock(total)} · −${formatClock(left)}`}
        </span>
      </div>
      <div className="row transport">
        <button type="button" className="btn big" disabled={!can} onClick={() => onOp("prev")} aria-label="Previous file">
          |◀
        </button>
        {playing ? (
          <button type="button" className="btn big primary" disabled={!can} onClick={() => onOp("pause")}>
            ❚❚ Pause
          </button>
        ) : (
          <button type="button" className="btn big primary" disabled={!can || !name || recording} onClick={() => onOp("play")}>
            ▶ Play
          </button>
        )}
        <button
          type="button"
          className="btn big"
          disabled={!can || st === "STOP"}
          onClick={() => (recording ? setStopping(true) : onOp("stop"))}
        >
          ■ Stop
        </button>
        <button type="button" className="btn big" disabled={!can} onClick={() => onOp("next")} aria-label="Next file">
          ▶|
        </button>
        <label className="check">
          <input
            type="checkbox"
            disabled={!can}
            checked={Number(autoplay?.value ?? 0) === 1}
            onChange={(e) => store.set("/config/tape/autoplay", e.target.checked ? 1 : 0)}
          />
          Then the next file of the folder
        </label>
      </div>
      {recording && (
        <p className="note">
          {ours
            ? "The console records the backup of tabula's recording to the stick (Record page): Stop recording stops it."
            : "The console records to the stick (from its own USB recorder screen)."}
        </p>
      )}
      {stopping && (
        <ConfirmDialog
          title="The console is recording to its USB stick: stop it?"
          confirm="■ Stop"
          onCancel={() => setStopping(false)}
          onConfirm={() => {
            setStopping(false);
            onOp("stop");
          }}
        >
          <p>
            {ours
              ? "It is the backup of tabula's recording: Stop ends it, while the recording goes on without it."
              : "The recording was started on the console (its USB recorder screen): Stop ends it there."}
          </p>
        </ConfirmDialog>
      )}
    </section>
  );
}
