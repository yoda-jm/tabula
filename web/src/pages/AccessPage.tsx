// Access links: the engineer gives each musician a link to their own in-ear
// mix, and viewers or another engineer device a link of their role. A link
// opened on a phone (or its QR code scanned) signs that phone in.

import { useCallback, useEffect, useState } from "react";
import qrcode from "qrcode-generator";
import { ConfirmDialog } from "../components/shell";
import { ROLE_LABELS } from "../lib/access";
import type { AccessStatus, Grant, Role } from "../lib/api";
import { accessApi } from "../lib/api";
import { ALL_STRIPS } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useValue } from "../store/hooks";
import { store } from "../store/store";

const BUSES = ALL_STRIPS.filter((s) => s.kind === "bus");

export function AccessPage() {
  const [st, setSt] = useState<AccessStatus | null>(null);
  const [error, setError] = useState("");
  const [base, setBase] = useState("");
  const [shown, setShown] = useState<string | null>(null); // grant whose QR code is open
  const [removing, setRemoving] = useState<Grant | null>(null);

  const refresh = useCallback(() => {
    accessApi.list().then(
      (s) => {
        setSt(s);
        setError("");
      },
      (e: Error) => setError(e.message),
    );
  }, []);
  useEffect(refresh, [refresh]);
  useEffect(() => {
    if (st && !st.urls.includes(base)) setBase(st.urls[0] ?? window.location.origin);
  }, [st, base]);

  const link = (g: Grant) => `${base}/a/${g.token}`;
  const setWrite = (on: boolean) =>
    accessApi.settings(on).then(setSt, (e: Error) => store.toast(e.message, "error"));
  const remove = (g: Grant) =>
    accessApi.remove(g.id).then(refresh, (e: Error) => store.toast(e.message, "error"));

  return (
    <div className="page access">
      <h2>Access</h2>
      <p className="muted">
        This machine always has full control. Other devices need a link: musicians get their own in-ear mix only,
        viewers see everything and change nothing.
      </p>
      {error && <p className="banner error">{error}</p>}
      {st && !st.reachable && (
        <p className="warn-box">
          tabula only listens on this machine, so phones cannot reach it. Start it with <code>-http :8033</code> to
          serve the whole network.
        </p>
      )}
      {st && (
        <section className="card section">
          <header className="section-head">
            <h3>Settings</h3>
          </header>
          <label className="check">
            <input type="checkbox" checked={st.musicians_can_write} onChange={(e) => void setWrite(e.target.checked)} />{" "}
            Musicians can change their mix (even when writes are not armed here)
          </label>
          <p className="muted">
            Devices without a link: {ROLE_LABELS[st.guest].toLowerCase()} (set with <code>-guest</code>).
          </p>
          {st.urls.length > 1 && (
            <label className="form-field">
              <span className="field-label">Links use</span>
              <select className="select" value={base} onChange={(e) => setBase(e.target.value)}>
                {st.urls.map((u) => (
                  <option key={u} value={u}>
                    {u}
                  </option>
                ))}
              </select>
            </label>
          )}
        </section>
      )}
      <NewAccess onAdded={refresh} />
      {st && (
        <section className="card section">
          <header className="section-head">
            <h3>Links</h3>
          </header>
          {st.grants.length === 0 && <p className="muted">No access link yet.</p>}
          <ul className="grants">
            {st.grants.map((g) => (
              <li key={g.id} className="grant">
                <div className="grant-head">
                  <strong>{g.name}</strong>
                  <span className="muted">
                    {ROLE_LABELS[g.role]}
                    {g.role === "musician" && g.bus ? <BusName n={g.bus} /> : null}
                  </span>
                </div>
                <div className="grant-link">
                  <code className="mono">{link(g)}</code>
                </div>
                <div className="grant-actions">
                  <button type="button" className="btn" onClick={() => setShown(shown === g.id ? null : g.id)}>
                    {shown === g.id ? "Hide QR code" : "QR code"}
                  </button>
                  <CopyButton text={link(g)} />
                  <button type="button" className="btn danger-text" onClick={() => setRemoving(g)}>
                    Delete
                  </button>
                </div>
                {shown === g.id && <QrCode text={link(g)} />}
              </li>
            ))}
          </ul>
        </section>
      )}
      {removing && (
        <ConfirmDialog
          title={`Delete the access of ${removing.name}?`}
          confirm="Delete"
          onConfirm={() => {
            void remove(removing);
            setRemoving(null);
          }}
          onCancel={() => setRemoving(null)}
        >
          Pages opened with this link stop at once. A new link can be made later.
        </ConfirmDialog>
      )}
    </div>
  );
}

function BusName({ n }: { n: number }) {
  const name = prettyText(useValue(`/bus/${String(n).padStart(2, "0")}/config/name`)?.text);
  return <>{`, bus ${n}${name ? " (" + name + ")" : ""}`}</>;
}

function NewAccess({ onAdded }: { onAdded(): void }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("musician");
  const [bus, setBus] = useState(1);
  const [busy, setBusy] = useState(false);
  const add = async () => {
    setBusy(true);
    try {
      await accessApi.add(name, role, role === "musician" ? bus : undefined);
      setName("");
      onAdded();
    } catch (e) {
      store.toast((e as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card section">
      <header className="section-head">
        <h3>New link</h3>
      </header>
      <form
        className="new-access"
        onSubmit={(e) => {
          e.preventDefault();
          void add();
        }}
      >
        <label className="form-field">
          <span className="field-label">Name</span>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Remy" required />
        </label>
        <label className="form-field">
          <span className="field-label">Role</span>
          <select className="select" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {(["musician", "viewer", "engineer"] as Role[]).map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </select>
        </label>
        {role === "musician" && (
          <label className="form-field">
            <span className="field-label">Mix</span>
            <select className="select" value={bus} onChange={(e) => setBus(Number(e.target.value))}>
              {BUSES.map((b, i) => (
                <BusChoice key={b.path} path={b.path} n={i + 1} />
              ))}
            </select>
          </label>
        )}
        <button type="submit" className="btn" disabled={busy || !name.trim()}>
          Create
        </button>
      </form>
    </section>
  );
}

function BusChoice({ path, n }: { path: string; n: number }) {
  const name = prettyText(useValue(`${path}/config/name`)?.text);
  return <option value={n}>{`Bus ${n}${name ? " — " + name : ""}`}</option>;
}

function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    } catch {
      store.toast("The browser does not allow copying here: select the link instead.", "warn");
    }
  };
  return (
    <button type="button" className="btn" onClick={copy}>
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** QrCode draws text as a QR code, in SVG. */
export function QrCode({ text }: { text: string }) {
  const qr = qrcode(0, "M");
  qr.addData(text);
  qr.make();
  const n = qr.getModuleCount();
  const cells: string[] = [];
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) if (qr.isDark(r, c)) cells.push(`M${c + 4} ${r + 4}h1v1h-1z`);
  }
  return (
    <svg className="qr" viewBox={`0 0 ${n + 8} ${n + 8}`} role="img" aria-label={`QR code of ${text}`}>
      <rect width={n + 8} height={n + 8} fill="#fff" />
      <path d={cells.join("")} fill="#000" />
    </svg>
  );
}
