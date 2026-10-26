// The backup on the console's USB stick: with it on, Start recording also
// starts the console's own recorder, which writes a stereo WAV file of what
// feeds it (the main mix by default) to its stick, by itself. Stop
// recording stops it. Its state comes from the console (/-stat/tape).

import { useState } from "react";
import type { Record as RecordStatus } from "../lib/session";
import { sessionApi } from "../lib/session";
import { TAPE_STATES, dropouts, formatClock, recordsToStick } from "../lib/usb";
import { useStatus, useValue } from "../store/hooks";
import { store } from "../store/store";

export function UsbBackupCard({ rec }: { rec: RecordStatus | null }) {
  const status = useStatus();
  const mounted = useValue("/-stat/usbmounted");
  const state = useValue("/-stat/tape/state");
  const file = useValue("/-stat/tape/file");
  const etime = useValue("/-stat/tape/etime");
  const rtime = useValue("/-stat/tape/rtime");
  const [busy, setBusy] = useState(false);
  const usb = rec?.usb;
  if (!rec || !usb) return null;

  const turn = async (on: boolean) => {
    setBusy(true);
    try {
      store.setRecord(await sessionApi.usbBackup(on));
    } catch (e) {
      store.toast((e as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };
  const st = String(state?.text ?? "STOP").trim();
  const path = String(file?.value ?? "");
  const lost = dropouts(Number(rtime?.value ?? 0));
  const ours = !!usb.file && usb.file === path && !usb.already;
  let live = "";
  if (status.connected && !usb.starting) {
    if (Number(mounted?.value ?? 0) !== 1) live = "No USB stick in the console.";
    else if (recordsToStick(st)) live = "";
    else if (st === "PLAY" || st === "PPAUSE") live = `The console's player is ${TAPE_STATES[st]} ${path.split("/").pop()}.`;
    else live = "USB stick in, the console's recorder is stopped.";
  }

  return (
    <section className="card usb-backup">
      <div className="section-head">
        <h2>Backup on the console's USB stick</h2>
        <span className="muted">{usb.on ? "started with every recording" : "off"}</span>
      </div>
      <label className="check">
        <input type="checkbox" checked={usb.on} disabled={busy} onChange={(e) => void turn(e.target.checked)} />
        Start the console's own recorder with every recording
      </label>
      <p className="muted">
        The console writes what feeds its recorder (the main mix, unless set otherwise on the console) to its USB stick as a stereo
        WAV file, by itself: it goes on if the laptop fails. Stop recording stops it. Music played from the stick is not interrupted:
        the backup is then not started.
      </p>
      {status.connected && recordsToStick(st) && (
        <p className="usb-rec">
          <strong className="danger-text">● {st === "RPAUSE" ? "REC PAUSED" : "REC"}</strong>{" "}
          <span className="mono">{formatClock(Number(etime?.value ?? 0))}</span> <span className="mono">{path}</span>
          {lost > 0 && <strong className="danger-text"> · ⚠ {lost} dropout{lost > 1 ? "s" : ""}</strong>}
          <span className="muted">
            {ours ? " · tabula's backup of this recording" : " · started on the console: Stop recording leaves it on"}
          </span>
        </p>
      )}
      {live && <p className="muted">{live}</p>}
      {usb.starting && <p className="muted">Starting the console's recorder…</p>}
      {usb.error && (
        <p className="warn-box" role="status">
          ⚠ {rec.recording ? "This recording has no backup on the stick" : "The last recording had no backup on the stick"}:{" "}
          {usb.error}
        </p>
      )}
      {!rec.recording && usb.file && !recordsToStick(st) && (
        <p className="muted">
          Last backup: <code>{usb.file}</code> on the stick.
        </p>
      )}
    </section>
  );
}
