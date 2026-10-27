// App shell pieces: confirmation dialog, toasts, connection pill, writes
// armed toggle, recording indicator, banners.

import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { api } from "../lib/api";
import { audioTrouble } from "../lib/audio";
import { formatDuration } from "../lib/session";
import { useRecord, useSceneLoad, useStatus, useStream, useToasts } from "../store/hooks";
import { store } from "../store/store";

export function ConfirmDialog(props: {
  title: string;
  children: ReactNode;
  confirm: string;
  onConfirm(): void;
  onCancel(): void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  const onCancel = useRef(props.onCancel);
  onCancel.current = props.onCancel;
  useEffect(() => {
    cancel.current?.focus(); // the safe choice has the focus
    const esc = (e: KeyboardEvent) => e.key === "Escape" && onCancel.current();
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, []);
  return (
    <div className="overlay" onClick={props.onCancel}>
      <div
        className="dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="dialog-title"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="dialog-title">{props.title}</h2>
        <div className="dialog-body">{props.children}</div>
        <div className="dialog-actions">
          <button ref={cancel} type="button" className="btn" onClick={props.onCancel}>
            Cancel
          </button>
          <button type="button" className="btn danger" onClick={props.onConfirm}>
            {props.confirm}
          </button>
        </div>
      </div>
    </div>
  );
}

export function Toasts() {
  const toasts = useToasts();
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <button key={t.id} type="button" className={"toast " + t.kind} onClick={() => store.dismiss(t.id)}>
          {t.text}
        </button>
      ))}
    </div>
  );
}

export function ConnectionPill({ onClick }: { onClick(): void }) {
  const s = useStatus();
  const stream = useStream();
  let cls = "off";
  let text = "Not connected";
  const c = s.console;
  if (stream === "down") {
    cls = "bad";
    text = "tabula unreachable";
  } else if (s.connecting) {
    cls = "busy";
    text = c && c.total ? `Syncing ${Math.floor((100 * c.synced) / c.total)}%` : "Connecting…";
  } else if (c && !c.connected) {
    cls = "bad";
    text = `${c.info.name || c.addr}: no answer`;
  } else if (s.connected && c) {
    cls = "ok";
    text = [c.info.name, c.info.model, c.info.firmware && "fw " + c.info.firmware].filter(Boolean).join(" · ");
  } else if (s.error) {
    cls = "bad";
    text = "Connection failed";
  }
  return (
    <button type="button" className={"pill " + cls} onClick={onClick} title={c?.addr ?? s.error ?? ""}>
      <span className="dot" aria-hidden />
      <span className="pill-text">{text}</span>
      {s.connecting && c && c.total > 0 && (
        <span className="pill-progress" style={{ width: `${(100 * c.synced) / c.total}%` }} aria-hidden />
      )}
    </button>
  );
}

export function ArmControl() {
  const s = useStatus();
  const load = useSceneLoad();
  const [asking, setAsking] = useState(false);
  const armed = !!s.console?.armed;
  // disarmed while a scene file loads: tabula sends it to its end first
  const pending = armed && !!load && !load.done && !!load.disarm_after;
  const set = async (on: boolean) => {
    setAsking(false);
    try {
      const st = await api.arm(on);
      store.setStatus(st);
      if (!on && st.console?.armed) {
        store.toast(`Writes disarm once ${load?.file ?? "the scene file"} is fully loaded: half a scene would be worse.`, "warn");
      }
    } catch (e) {
      store.toast(`Arm: ${(e as Error).message}`, "error");
    }
  };
  return (
    <>
      <button
        type="button"
        className={"arm " + (armed ? "armed" : "safe")}
        aria-pressed={armed}
        disabled={!s.connected}
        onClick={() => (pending ? void set(true) : armed ? void set(false) : setAsking(true))}
        title={
          pending
            ? "A scene file is being sent to the console: writes disarm once it is fully loaded. Click to keep them armed."
            : armed
              ? "Writes go to the console. Click to go back to read-only."
              : "Read-only. Click to arm writes."
        }
      >
        <span className="arm-state">{armed ? "WRITES ARMED" : "READ-ONLY"}</span>
        <span className="arm-action">{pending ? "Disarms after the load" : armed ? "Disarm" : "Arm"}</span>
      </button>
      {asking && (
        <ConfirmDialog title="Arm writes?" confirm="Arm writes" onConfirm={() => void set(true)} onCancel={() => setAsking(false)}>
          <p>
            <strong>Writes go to the live console.</strong> Every fader, button and field you touch changes the sound at once,
            for every other remote app too.
          </p>
          <p className="muted">Connected to {s.console?.info.name || s.console?.addr}.</p>
        </ConfirmDialog>
      )}
    </>
  );
}

/**
 * StreamBanner is a bar on every page while the event stream to tabula is
 * down: the values and meters shown are not live (the meters are cleared).
 */
export function StreamBanner() {
  const stream = useStream();
  if (stream !== "down") return null;
  return (
    <div className="stream-banner" role="alert">
      Connection lost — reconnecting…
    </div>
  );
}

/**
 * UpdateBanner asks to reload a page opened before tabula was updated (a
 * phone left open while the laptop got a new tabula): it still runs the old
 * GUI, which may not match the new tabula.
 */
export function UpdateBanner() {
  const s = useStatus();
  const was = store.firstVersion;
  if (!was || !s.version || s.version === was) return null;
  return (
    <div className="audio-banner warn update-banner" role="status">
      <strong>tabula was updated</strong>
      <span className="audio-banner-text">
        {was} → {s.version}: reload the page to use the new version.
      </span>
      <button type="button" className="btn" onClick={() => location.reload()}>
        Reload
      </button>
    </div>
  );
}

/**
 * AudioBanner is a bar under the navigation, on every page, while the audio
 * of a recording is not recorded (red) or in trouble (amber): the header's
 * REC is easy to miss during a show.
 */
export function AudioBanner({ onOpen }: { onOpen?: () => void }) {
  const r = useRecord();
  const trouble = r?.recording ? audioTrouble(r.audio) : null;
  if (!trouble) return null;
  const failed = trouble.level === "failed";
  return (
    <div className={"audio-banner " + trouble.level} role="alert">
      <strong>{failed ? "⚠ The audio is NOT recorded" : "⚠ Audio"}</strong>
      <span className="audio-banner-text">{failed ? `${/[.!?]$/.test(trouble.text) ? trouble.text : trouble.text + "."} The console still is.` : trouble.text}</span>
      {onOpen && (
        <button type="button" className="btn" onClick={onOpen}>
          {failed ? "Fix it" : "See"} on the Record page
        </button>
      )}
    </div>
  );
}

/** RecIndicator shows a red REC with the elapsed time while recording (and replay progress). */
export function RecIndicator({ onClick }: { onClick(): void }) {
  const r = useRecord();
  const [, tick] = useState(0);
  useEffect(() => {
    if (!r?.recording) return;
    const t = setInterval(() => tick((n) => n + 1), 500);
    return () => clearInterval(t);
  }, [r?.recording]);
  if (!r) return null;
  if (r.recording) {
    const elapsed = store.recordElapsed();
    const trouble = audioTrouble(r.audio);
    return (
      <button
        type="button"
        className={"rec" + (trouble?.level === "failed" ? " no-audio" : "")}
        onClick={onClick}
        title={trouble ? `Recording. Audio: ${trouble.text}` : "Recording"}
      >
        <span className="rec-dot" aria-hidden />
        REC <span className="rec-time">{formatDuration(elapsed)}</span>
        {trouble?.level === "failed" ? (
          <span className="rec-warn">⚠ NO AUDIO</span>
        ) : (
          trouble && <span aria-label={`audio: ${trouble.text}`}> ⚠</span>
        )}
      </button>
    );
  }
  if (r.replaying) {
    return (
      <button type="button" className="rec replay" onClick={onClick} title={`Replaying ${r.replay_of}`}>
        ▶ REPLAY <span className="rec-time">{formatDuration(r.replay_at ?? 0)}</span>
      </button>
    );
  }
  return null;
}
