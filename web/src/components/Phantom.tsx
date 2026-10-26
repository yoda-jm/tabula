// The phantom power (+48 V) switch of a headamp, and the question asked
// before switching it (lib/phantom.ts): on the Channel page's headamp and
// in the Browser's generic editors.

import { useState } from "react";
import type { PhantomAsk } from "../lib/phantom";
import { phantomAsk, phantomPort } from "../lib/phantom";
import { useCanWrite, useValue } from "../store/hooks";
import { store } from "../store/store";
import { ConfirmDialog } from "./shell";

/** PhantomDialog asks before switching phantom power on port; ask was taken when it opened. */
export function PhantomDialog({ ask, port, onConfirm, onCancel }: { ask: PhantomAsk; port: string; onConfirm(): void; onCancel(): void }) {
  return (
    <ConfirmDialog title={ask.title} confirm={ask.confirm} onConfirm={onConfirm} onCancel={onCancel}>
      <p>
        {ask.verb} +48 V on <strong>{port}</strong> makes a loud pop if the channel is open: pull its fader down first.
      </p>
      {ask.target === 1 && <p className="muted">Ribbon microphones and some line sources can be damaged by phantom power.</p>}
    </ConfirmDialog>
  );
}

/** PhantomToggle switches +48 V after a confirmation: it pops through the PA. */
export function PhantomToggle({ addr, port }: { addr: string; port?: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  // the question, and so the value written, is taken when the dialog opens: the switch may change elsewhere meanwhile
  const [ask, setAsk] = useState<PhantomAsk | null>(null);
  const on = Number(val?.value) === 1;
  return (
    <>
      <button
        type="button"
        className={"btn toggle phantom" + (on ? " lit" : "")}
        aria-pressed={on}
        disabled={!can || val === undefined}
        onClick={() => setAsk(phantomAsk(on ? 0 : 1))}
      >
        {on ? "48 V ON" : "OFF"}
      </button>
      {ask && (
        <PhantomDialog
          ask={ask}
          port={port ?? phantomPort(addr)}
          onConfirm={() => {
            setAsk(null);
            store.set(addr, ask.target);
          }}
          onCancel={() => setAsk(null)}
        />
      )}
    </>
  );
}
