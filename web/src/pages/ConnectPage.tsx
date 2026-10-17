// Connection setup: find consoles on the network, or type an address.

import { useState } from "react";
import type { Discovered } from "../lib/api";
import { api } from "../lib/api";
import { usePersistent } from "../lib/route";
import { useStatus } from "../store/hooks";
import { store } from "../store/store";

export function ConnectPage() {
  const s = useStatus();
  const [addr, setAddr] = usePersistent("tabula.addr", "127.0.0.1");
  const [found, setFound] = useState<Discovered[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState("");
  const c = s.console;

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
            <button type="button" className="btn" onClick={() => void disconnect()}>
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
            void connect(addr);
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
          <button type="submit" className="btn primary" disabled={!addr.trim() || s.connecting}>
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
                <button type="button" className="btn found-item" onClick={() => void connect(d.ip)} disabled={s.connecting}>
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
    </div>
  );
}
