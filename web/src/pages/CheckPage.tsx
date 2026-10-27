// The pre-show check: before a show, what can spoil it (the console, the
// card's mode and rate against the audio settings, the device, the disk for
// the planned length, the power, sleep, the console's USB stick, the
// phones), each line with what to do; checked again every few seconds.

import { useCallback, useEffect, useState } from "react";
import type { CheckView } from "../lib/check";
import { CHECK_HOURS, LEVEL_LABELS, checkApi, checkSummary } from "../lib/check";
import { go, usePersistent } from "../lib/route";

/** checkEvery is how often the page checks again. */
const checkEvery = 5000;

/** useCheck checks now, then every checkEvery while mounted. */
export function useCheck(hours: number, every = checkEvery) {
  const [view, setView] = useState<CheckView | null>(null);
  const [error, setError] = useState("");
  const refresh = useCallback(async () => {
    try {
      setView(await checkApi.get(hours));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [hours]);
  useEffect(() => {
    void refresh();
    const t = setInterval(() => void refresh(), every);
    return () => clearInterval(t);
  }, [refresh, every]);
  return { view, error, refresh };
}

export function CheckPage() {
  const [hours, setHours] = usePersistent(CHECK_HOURS, 3);
  const [typed, setTyped] = useState(String(hours));
  const { view, error, refresh } = useCheck(hours);
  const sum = view ? checkSummary(view) : null;
  const commit = () => {
    const h = Number(typed.replace(",", "."));
    if (Number.isFinite(h) && h > 0) setHours(Math.min(24, Math.max(0.25, h)));
    else setTyped(String(hours));
  };
  return (
    <div className="page precheck">
      <section className="card">
        <header className="section-head">
          <h2>Pre-show check</h2>
          {sum && <span className={"check-sum " + sum.level}>{sum.text}</span>}
        </header>
        <p className="muted">
          What can spoil the recording or the show, checked again every few seconds: fix the red lines before the doors open.
        </p>
        <form
          className="row check-form"
          onSubmit={(e) => {
            e.preventDefault();
            commit();
            void refresh();
          }}
        >
          <label className="form-field">
            <span className="field-label">The show lasts (hours)</span>
            <input
              className="input num"
              type="number"
              inputMode="decimal"
              min={0.25}
              max={24}
              step={0.25}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onBlur={commit}
            />
          </label>
          <button type="submit" className="btn">
            Check again
          </button>
          {view && <span className="muted">Checked at {new Date(view.at).toLocaleTimeString()}</span>}
        </form>
        {error && <p className="error">{error}</p>}
      </section>
      {view && <CheckLines view={view} />}
    </div>
  );
}

/** CheckLines are the lines of a check, each with its level, what is found and what to do. */
export function CheckLines({ view }: { view: CheckView }) {
  return (
    <ul className="check-lines">
      {view.lines.map((l) => (
        <li key={l.id} className={"check-line " + l.level}>
          <span className="check-dot" role="img" aria-label={LEVEL_LABELS[l.level]} title={LEVEL_LABELS[l.level]} />
          <div className="check-body">
            <strong>{l.title}</strong>
            <p>{l.text}</p>
            {l.fix && <p className="check-fix">{l.fix}</p>}
          </div>
        </li>
      ))}
    </ul>
  );
}

/** CheckSummaryLine is the Record page's line: the check's summary, opening the Check page. */
export function CheckSummaryLine() {
  const [hours] = usePersistent(CHECK_HOURS, 3);
  const { view } = useCheck(hours, 15000);
  if (!view) return null;
  const sum = checkSummary(view);
  return (
    <p className={"check-summary " + sum.level}>
      <span className="check-dot" aria-hidden /> Pre-show check: {sum.text}.{" "}
      <a
        href="#/check"
        onClick={(e) => {
          e.preventDefault();
          go({ page: "check" });
        }}
      >
        {sum.level === "ok" ? "Details" : "See what to do"}
      </a>
    </p>
  );
}
