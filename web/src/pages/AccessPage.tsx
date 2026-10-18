// Access links: the engineer gives each musician a link to their own in-ear
// mix, and viewers or another engineer device a link of their role. A link
// opened on a phone (or its QR code scanned) signs that phone in. The
// engineer password, when set, signs browsers in as the engineer (this
// machine included); they are listed as signed-in devices.

import { useCallback, useEffect, useState } from "react";
import qrcode from "qrcode-generator";
import { ConfirmDialog } from "../components/shell";
import { ROLE_LABELS } from "../lib/access";
import type { AccessStatus, Grant, Role } from "../lib/api";
import { accessApi, api } from "../lib/api";
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
  const links = st?.grants.filter((g) => !g.session) ?? [];
  const sessions = st?.grants.filter((g) => g.session) ?? [];
  const setWrite = (on: boolean) =>
    accessApi.settings(on).then(setSt, (e: Error) => store.toast(e.message, "error"));
  const remove = (g: Grant) =>
    accessApi.remove(g.id).then(refresh, (e: Error) => store.toast(e.message, "error"));

  return (
    <div className="page access">
      <h2>Access</h2>
      <p className="muted">
        {st?.password
          ? "Every browser, this machine included, signs in with the engineer password to have full control. "
          : "This machine has full control. "}
        Other devices need a link: musicians get their own in-ear mix only, viewers see everything and change nothing.
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
      {st && <PasswordCard st={st} onChange={setSt} />}
      {st && (st.password || sessions.length > 0) && <Sessions sessions={sessions} current={st.current} onChange={refresh} />}
      <NewAccess onAdded={refresh} />
      {st && (
        <section className="card section">
          <header className="section-head">
            <h3>Links</h3>
          </header>
          {links.length === 0 && <p className="muted">No access link yet.</p>}
          <ul className="grants">
            {links.map((g) => (
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

/** PasswordCard sets, changes or removes the engineer password. */
function PasswordCard({ st, onChange }: { st: AccessStatus; onChange(s: AccessStatus): void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState(false);
  const mismatch = again !== "" && next !== again;
  const blocked = st.guest === "engineer";

  const send = async (password: string, done: string) => {
    setBusy(true);
    try {
      const had = st.password;
      onChange(await accessApi.password(current, password));
      setCurrent("");
      setNext("");
      setAgain("");
      store.toast(done, "info");
      if (!had) void store.loadAccess(); // the first password signed this browser in
    } catch (e) {
      store.toast((e as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };
  const change = () =>
    void send(
      next,
      st.password ? "Password changed: the other signed-in devices are signed out." : "Password set: this browser is signed in.",
    );

  return (
    <section className="card section">
      <header className="section-head">
        <h3>Engineer password</h3>
      </header>
      {st.password ? (
        <p className="muted">
          Set: every browser signs in with it to have full control, once (it remembers). Changing or removing it signs
          out the other devices.
        </p>
      ) : (
        <p className="muted">
          None: whoever uses this machine has full control, and so does everyone if tabula is reached through a proxy
          on this machine. With a password, every browser signs in, this machine included (once: it remembers).
        </p>
      )}
      {blocked ? (
        <p className="warn-box">
          Every device is the engineer (<code>-guest engineer</code>): a password would protect nothing.
        </p>
      ) : (
        <form
          className="new-access"
          onSubmit={(e) => {
            e.preventDefault();
            change();
          }}
        >
          <input type="text" name="username" autoComplete="username" value="engineer" readOnly hidden />
          {st.password && (
            <label className="form-field">
              <span className="field-label">Current password</span>
              <input
                className="input"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </label>
          )}
          <label className="form-field">
            <span className="field-label">{st.password ? "New password" : "Password"}</span>
            <input
              className="input"
              type="password"
              autoComplete="new-password"
              minLength={8}
              placeholder="8 characters or more"
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </label>
          <label className="form-field">
            <span className="field-label">Again</span>
            <input
              className="input"
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
              aria-invalid={mismatch}
            />
          </label>
          <button
            type="submit"
            className="btn"
            disabled={busy || next.length < 8 || next !== again || (st.password && !current)}
          >
            {st.password ? "Change" : "Set password"}
          </button>
          {st.password && (
            <button
              type="button"
              className="btn danger-text"
              disabled={busy || !current}
              title={current ? undefined : "Enter the current password first"}
              onClick={() => setRemoving(true)}
            >
              Remove
            </button>
          )}
        </form>
      )}
      {mismatch && <p className="danger-text">The two passwords differ.</p>}
      {removing && (
        <ConfirmDialog
          title="Remove the engineer password?"
          confirm="Remove"
          onConfirm={() => {
            setRemoving(false);
            void send("", "Password removed.");
          }}
          onCancel={() => setRemoving(false)}
        >
          Whoever uses this machine gets full control again without signing in, and the other signed-in devices are
          signed out.
        </ConfirmDialog>
      )}
    </section>
  );
}

/** Sessions lists the browsers signed in with the engineer password. */
function Sessions({ sessions, current, onChange }: { sessions: Grant[]; current?: string; onChange(): void }) {
  const signOut = async (g: Grant) => {
    try {
      if (g.id === current) {
        await api.logout();
        window.location.reload();
        return;
      }
      await accessApi.remove(g.id);
      onChange();
    } catch (e) {
      store.toast((e as Error).message, "error");
    }
  };
  return (
    <section className="card section">
      <header className="section-head">
        <h3>Signed-in devices</h3>
      </header>
      {sessions.length === 0 && <p className="muted">No device is signed in with the password.</p>}
      <ul className="grants">
        {sessions.map((g) => (
          <li key={g.id} className="grant">
            <div className="grant-head">
              <strong>{g.name}</strong>
              <span className="muted">
                since {new Date(g.created).toLocaleString()}
                {g.id === current ? ", this browser" : ""}
              </span>
            </div>
            <div className="grant-actions">
              <button type="button" className="btn danger-text" onClick={() => void signOut(g)}>
                Sign out
              </button>
            </div>
          </li>
        ))}
      </ul>
    </section>
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
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Theo" required />
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
