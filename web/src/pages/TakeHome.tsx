// Taking a show home, in a session's details on the Record page: the
// session as one zip, or copied to a disk plugged into the machine running
// tabula, its tracks as recorded (WAV) or in FLAC (lib/takehome.ts).

import { useCallback, useEffect, useState } from "react";
import { formatBytes, plural } from "../lib/audio";
import type { SessionDetail } from "../lib/session";
import type { CopyStatus, Disks } from "../lib/takehome";
import { copyText, diskLabel, homeApi, roomFor } from "../lib/takehome";

export function TakeHome({ detail, recording, recordingThis }: { detail: SessionDetail; recording: boolean; recordingThis: boolean }) {
  const [flac, setFlac] = useState(false);
  const [disks, setDisks] = useState<Disks | null>(null);
  const [disk, setDisk] = useState("");
  const [copy, setCopy] = useState<CopyStatus | null>(null);
  const [error, setError] = useState("");

  const look = useCallback(async () => {
    try {
      const d = await homeApi.disks();
      setDisks(d);
      setDisk((cur) => (d.disks.some((x) => x.path === cur && x.writable) ? cur : (d.disks.find((x) => x.writable)?.path ?? "")));
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    void look();
    homeApi.status().then(setCopy, () => {});
  }, [look]);
  // a copy running is followed; once over, the disk's room changed
  const running = copy?.state === "copying";
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => homeApi.status().then(setCopy, () => {}), 1000);
    return () => clearInterval(t);
  }, [running]);
  useEffect(() => {
    if (copy && copy.state !== "copying") void look();
  }, [copy?.state, look]);

  const name = detail.name;
  const tracks = detail.meta.audio?.tracks?.length ?? 0;
  const flacSize = detail.flac_size ?? 0;
  const canFlac = !!disks?.flac && flacSize > 0;
  const useFlac = flac && canFlac;
  const need = useFlac ? flacSize : (detail.size ?? 0);
  const about = useFlac ? "about " : "";
  const target = disks?.disks.find((d) => d.path === disk);
  const room = target ? roomFor(target, need) : "unknown";

  const start = async () => {
    setError("");
    try {
      setCopy(await homeApi.copy(name, disk, useFlac));
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const stop = async () => {
    try {
      setCopy(await homeApi.stop());
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <>
      <h4>Take it home</h4>
      <p className="muted">
        The whole session folder in one go: the console at the start, its changes and markers, the exports
        {tracks ? ` and the ${plural(tracks, "track")}` : ""}. Nothing is written on the disk of the machine running tabula on the
        way.
      </p>
      {recordingThis ? (
        <p className="warn-box">This session is still being recorded: it can be taken home once the recording stops.</p>
      ) : (
        <>
          <div className="row">
            {flacSize > 0 && (
              <div className="seg" role="group" aria-label="Tracks taken home as">
                <button type="button" className={"btn seg-btn" + (!useFlac ? " lit" : "")} aria-pressed={!useFlac} onClick={() => setFlac(false)}>
                  WAV, as recorded · {formatBytes(detail.size ?? 0)}
                </button>
                <button
                  type="button"
                  className={"btn seg-btn" + (useFlac ? " lit" : "")}
                  aria-pressed={useFlac}
                  disabled={!canFlac}
                  title={canFlac ? "Lossless, about half the size; converted while it is sent or copied" : "Needs ffmpeg on the machine running tabula"}
                  onClick={() => setFlac(true)}
                >
                  FLAC · about {formatBytes(flacSize)}
                </button>
              </div>
            )}
            <a
              className="btn"
              href={homeApi.zipUrl(name, useFlac)}
              download
              title="One zip of the session folder, made while it downloads: this device needs the room for it"
            >
              ⤓ Download as a zip ({about}
              {formatBytes(need)})
            </a>
          </div>
          {flacSize > 0 && disks && !disks.flac && <p className="muted">FLAC needs ffmpeg on the machine running tabula.</p>}
          <p className="field-label copy-to">Or copy it to a disk plugged into the machine running tabula:</p>
          <div className="row">
            <select
              className="select"
              aria-label="Disk to copy to"
              value={disk}
              onChange={(e) => setDisk(e.target.value)}
              disabled={!disks?.disks.length}
            >
              {!disk && <option value="">{disks?.disks.length ? "no disk to write on" : "no disk plugged in"}</option>}
              {disks?.disks.map((d) => (
                <option key={d.path} value={d.path} disabled={!d.writable}>
                  {diskLabel(d)}
                </option>
              ))}
            </select>
            <button type="button" className="btn" onClick={() => void look()}>
              Look again
            </button>
            <button type="button" className="btn primary" disabled={!target || room === "short" || running} onClick={() => void start()}>
              Copy to {target?.name ?? "the disk"}
            </button>
          </div>
          {disks && !disks.disks.length && (
            <p className="muted">Plug a USB disk into the machine running tabula, then Look again.</p>
          )}
          {target && room === "short" && (
            <p className="error">
              Not enough room on {target.name}: the session takes {about}
              {formatBytes(need)}, the disk has {formatBytes(target.free)} free.
            </p>
          )}
          {recording && (
            <p className="warn-box">
              A show is being recorded: copying or downloading reads the disk it records to. Better once the show is over.
            </p>
          )}
        </>
      )}
      {copy && <CopyProgress copy={copy} name={name} onStop={() => void stop()} />}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </>
  );
}

/** CopyProgress is the copy running (of this session or another) or the last copy of this one. */
function CopyProgress({ copy, name, onStop }: { copy: CopyStatus; name: string; onStop(): void }) {
  const other = copy.session !== name;
  if (copy.state === "copying") {
    return (
      <>
        {other && <p className="muted">Copying the session {copy.session} (one copy at a time):</p>}
        <div className="row copy-progress">
          <div className="progress grow" role="progressbar" aria-valuemin={0} aria-valuemax={copy.total} aria-valuenow={copy.done}>
            <div className="progress-bar" style={{ width: `${copy.total ? (100 * copy.done) / copy.total : 0}%` }} />
            <span className="progress-text">{copyText(copy)}</span>
          </div>
          <button type="button" className="btn danger-text" onClick={onStop}>
            Cancel
          </button>
        </div>
        <p className="muted">
          Into <code className="wrap">{copy.folder}</code>
          {copy.flac ? " in FLAC" : ""}; until it is complete, the folder's name ends with “(copying)”.
        </p>
      </>
    );
  }
  if (other) return null;
  switch (copy.state) {
    case "done":
      return (
        <p className="copy-done">
          ✓ Copied to <code className="wrap">{copy.folder}</code>. Eject the disk before unplugging it.
        </p>
      );
    case "cancelled":
      return <p className="muted">Copy cancelled: nothing was left on the disk.</p>;
  }
  return (
    <p className="error" role="alert">
      Copy failed: {copy.error}
    </p>
  );
}
