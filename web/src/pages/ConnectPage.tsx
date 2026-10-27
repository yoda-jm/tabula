// Connection setup: find consoles on the network, or type an address. A
// console that does not answer is asked again by tabula: another address
// can replace it at any time. Both end a running recording: asked first.

import { useEffect, useState } from "react";
import { ConfirmDialog } from "../components/shell";
import type { About, Discovered } from "../lib/api";
import { api } from "../lib/api";
import { linkQuality } from "../lib/link";
import { usePersistent } from "../lib/route";
import type { EndsRecording } from "../lib/session";
import { endsRecording, sinceText } from "../lib/session";
import { useRecord, useStatus } from "../store/hooks";
import { store } from "../store/store";

export function ConnectPage() {
  const s = useStatus();
  const [addr, setAddr] = usePersistent("tabula.addr", "127.0.0.1");
  const [found, setFound] = useState<Discovered[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState("");
  const rec = useRecord();
  const [ask, setAsk] = useState<(EndsRecording & { run(): void }) | null>(null);
  const c = s.console;

  /** unlessRecording runs f, or asks first when it would end a recording. */
  const unlessRecording = (action: "connect" | "disconnect", f: () => Promise<void>) => {
    const q = endsRecording(action, rec);
    if (q) setAsk({ ...q, run: () => void f() });
    else void f();
  };

  const discover = async () => {
    setSearching(true);
    setError("");
    try {
      setFound(await api.discover());
    } catch (e) {
      setError(`Search failed: ${(e as Error).message}`);
    } finally {
      setSearching(false);
    }
  };
  const connect = async (to: string) => {
    setError("");
    try {
      store.setStatus(await api.connect(to.trim()));
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const disconnect = async () => {
    setError("");
    try {
      store.setStatus(await api.disconnect());
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const progress = c && c.total ? c.synced / c.total : 0;
  return (
    <div className="page connect">
      <section className="card">
        <h2>Console</h2>
        {s.connected && c ? (
          <dl className="facts">
            <dt>Name</dt>
            <dd>{c.info.name || "—"}</dd>
            <dt>Model</dt>
            <dd>{c.info.model || "—"}</dd>
            <dt>Firmware</dt>
            <dd>{c.info.firmware || "—"}</dd>
            <dt>Address</dt>
            <dd>
              <code>{c.addr}</code>
            </dd>
            <dt>Synced</dt>
            <dd>
              {c.synced} / {c.total} nodes{c.missing?.length ? `, ${c.missing.length} never answered` : ""}
            </dd>
            <dt>Writes</dt>
            <dd>{c.armed ? <strong className="danger-text">armed</strong> : "read-only"}</dd>
            {c.link && (
              <>
                <dt>Link</dt>
                <dd className={"link-" + linkQuality(c.link).level}>{linkQuality(c.link).detail}</dd>
              </>
            )}
          </dl>
        ) : s.connecting ? (
          <p>Connecting to {c?.addr ?? addr}…</p>
        ) : (
          <p className="muted">Not connected.</p>
        )}
        {s.connecting && (
          <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress * 100)}>
            <div className="progress-bar" style={{ width: `${progress * 100}%` }} />
            <span className="progress-text">{c?.total ? `Sync ${c.synced} / ${c.total}` : "Waiting for the console…"}</span>
          </div>
        )}
        {(s.error || error) && (
          <p className="error" role="alert">
            {error || s.error}
          </p>
        )}
        {(c || s.connecting) && (
          <div className="row">
            <button type="button" className="btn" onClick={() => unlessRecording("disconnect", disconnect)}>
              Disconnect
            </button>
          </div>
        )}
      </section>

      <section className="card">
        <h2>Connect by address</h2>
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault();
            unlessRecording("connect", () => connect(addr));
          }}
        >
          <label className="grow">
            <span className="field-label">Console IP (port 10023 unless given)</span>
            <input
              className="input"
              type="text"
              inputMode="url"
              value={addr}
              spellCheck={false}
              autoComplete="off"
              onChange={(e) => setAddr(e.target.value)}
              placeholder="192.168.1.62"
            />
          </label>
          {/* enabled while connecting: a new address replaces one that does not answer */}
          <button type="submit" className="btn primary" disabled={!addr.trim()}>
            Connect
          </button>
        </form>
      </section>

      <section className="card">
        <h2>Find consoles</h2>
        <p className="muted">Asks the local network who is a console (broadcast, 1.5 s).</p>
        <div className="row">
          <button type="button" className="btn" onClick={() => void discover()} disabled={searching}>
            {searching ? "Searching…" : "Find consoles"}
          </button>
        </div>
        {found && found.length === 0 && <p className="muted">No console answered.</p>}
        {found && found.length > 0 && (
          <ul className="found">
            {found.map((d) => (
              <li key={d.ip}>
                <button type="button" className="btn found-item" onClick={() => unlessRecording("connect", () => connect(d.ip))}>
                  <strong>{d.name || d.ip}</strong>
                  <span className="muted">
                    {d.model} · fw {d.firmware} · {d.ip}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
      <AboutCard />
      {ask && (
        <ConfirmDialog
          title={ask.title}
          confirm={ask.confirm}
          onCancel={() => setAsk(null)}
          onConfirm={() => {
            setAsk(null);
            ask.run();
          }}
        >
          <p>
            <strong>{ask.body}</strong>
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** AboutCard says which tabula runs, since when, and where it keeps its files: its log first, for after a show. */
function AboutCard() {
  const [about, setAbout] = useState<About | null>(null);
  useEffect(() => {
    api.about().then(setAbout, () => setAbout(null));
  }, []);
  if (!about) return null;
  const started = new Date(about.started);
  return (
    <section className="card">
      <h2>This tabula</h2>
      <dl className="facts about">
        <dt>Version</dt>
        <dd>{about.version}</dd>
        <dt>Running since</dt>
        <dd>
          {started.toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })} ({sinceText(started)})
        </dd>
        <dt>Log</dt>
        <dd>{about.log ? <code>{about.log}</code> : <span className="muted">the terminal only (-log none)</span>}</dd>
        <dt>Sessions</dt>
        <dd>
          <code>{about.sessions}</code>
        </dd>
        <dt>Scene files</dt>
        <dd>
          <code>{about.scenes}</code>
        </dd>
        <dt>Access links</dt>
        <dd>{about.access ? <code>{about.access}</code> : <span className="muted">kept in memory only</span>}</dd>
        <dt>GUI</dt>
        <dd>
          <code>{about.listen}</code>
        </dd>
      </dl>
    </section>
  );
}

