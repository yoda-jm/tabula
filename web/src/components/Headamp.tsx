// The headamp feeding a channel (or aux input): which input it is, its gain
// and its phantom power. The console tells which headamp feeds each strip
// in /-ha/NN/index (-1 when the input has no preamp: card, user inputs,
// buses…).

import { useState } from "react";
import { haIndexAddr, headampPath, headampPort, inputSource, portLabel } from "../lib/routing";
import { sourceLabel } from "../lib/strips";
import { useCanWrite, useNodes, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { store } from "../store/store";
import { ConfirmDialog } from "./shell";
import { ParamControl } from "./controls";

export function Headamp({ strip }: { strip: string }) {
  const addr = haIndexAddr(strip);
  const idx = useValue(addr);
  if (!addr || idx === undefined) return null;
  const index = Number(idx.value);
  const port = headampPort(index);
  if (!port) return <NoHeadamp strip={strip} />;
  const ha = headampPath(index);
  return (
    <div className="headamp">
      <div className="fields">
        <div className="field">
          <span className="field-label">Input</span>
          <span className="headamp-port">
            <strong>{portLabel(port)}</strong> <span className="muted">headamp {index}</span>
          </span>
        </div>
        <div className="field">
          <span className="field-label">Gain</span>
          <ParamControl addr={`${ha}/gain`} />
        </div>
        <div className="field">
          <span className="field-label">Phantom (+48 V)</span>
          <PhantomToggle addr={`${ha}/phantom`} port={portLabel(port)} />
        </div>
      </div>
    </div>
  );
}

/** NoHeadamp says where an input without a preamp comes from. */
function NoHeadamp({ strip }: { strip: string }) {
  const src = useValue(`${strip}/config/source`);
  useNodes(["/config/routing", "/config/routing/IN", "/config/routing/PLAY"]);
  const n = Number(src?.value ?? 0);
  let from = "no input";
  if (strip.startsWith("/ch/") && n >= 1 && n <= 32) {
    const p = inputSource(lookup, n);
    from = p ? portLabel(p) : "OFF";
  } else if (n > 0) from = sourceLabel(n);
  return (
    <p className="muted headamp-none">
      No headamp: the input comes from <strong>{from}</strong>.
    </p>
  );
}

/** PhantomToggle switches +48 V after a confirmation: it pops through the PA. */
function PhantomToggle({ addr, port }: { addr: string; port: string }) {
  const val = useValue(addr);
  const can = useCanWrite(addr);
  const [asking, setAsking] = useState(false);
  const on = Number(val?.value) === 1;
  return (
    <>
      <button
        type="button"
        className={"btn toggle phantom" + (on ? " lit" : "")}
        aria-pressed={on}
        disabled={!can || val === undefined}
        onClick={() => setAsking(true)}
      >
        {on ? "48 V ON" : "OFF"}
      </button>
      {asking && (
        <ConfirmDialog
          title={on ? "Turn phantom power off?" : "Turn phantom power on?"}
          confirm={on ? "Turn off" : "Turn on +48 V"}
          onConfirm={() => {
            setAsking(false);
            store.set(addr, on ? 0 : 1);
          }}
          onCancel={() => setAsking(false)}
        >
          <p>
            {on ? "Removing" : "Sending"} +48 V on <strong>{port}</strong> makes a loud pop if the channel is open: pull its
            fader down first.
          </p>
          {!on && <p className="muted">Ribbon microphones and some line sources can be damaged by phantom power.</p>}
        </ConfirmDialog>
      )}
    </>
  );
}
