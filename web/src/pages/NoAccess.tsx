// The page of a browser without access: with an engineer password set, the
// engineer signs in here (this machine included); others need an access link.

import { useState } from "react";
import { api } from "../lib/api";
import { useAccess } from "../store/hooks";

export function NoAccess() {
  const who = useAccess();
  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">tabula</span>
      </header>
      <main className="main">
        <div className="page">
          <div className="card noaccess">{who?.password ? <SignIn /> : <NoLink />}</div>
        </div>
      </main>
    </div>
  );
}

function NoLink() {
  return (
    <>
      <h2>No access</h2>
      <p>This device cannot see the console. Ask the engineer for an access link: a QR code to scan, or a link to open here.</p>
    </>
  );
}

function SignIn() {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      await api.login(password);
      window.location.reload(); // start again signed in: schema, state and events
    } catch (e) {
      const msg = (e as Error).message;
      setError(msg.charAt(0).toUpperCase() + msg.slice(1) + ".");
      setPassword("");
      setBusy(false);
    }
  };
  return (
    <>
      <h2>Sign in</h2>
      <p>Engineer: enter the password set on the Access page.</p>
      <form
        className="signin"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {/* lets password managers keep the password under a name */}
        <input type="text" name="username" autoComplete="username" value="engineer" readOnly hidden />
        <label className="form-field">
          <span className="field-label">Password</span>
          <input
            className="input"
            type="password"
            name="password"
            autoComplete="current-password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit" className="btn" disabled={busy || !password}>
          Sign in
        </button>
      </form>
      {error && (
        <p className="banner error" role="alert">
          {error}
        </p>
      )}
      <p className="muted">Musicians and viewers: ask the engineer for an access link (a QR code to scan).</p>
    </>
  );
}
