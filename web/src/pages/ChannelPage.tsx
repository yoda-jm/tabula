// The selected strip in detail: only the sections its type has.

import { EqCurve } from "../components/EqCurve";
import { Headamp } from "../components/Headamp";
import { ParamControl } from "../components/controls";
import { Scribble } from "../components/Scribble";
import { inputSource, portLabel } from "../lib/routing";
import type { Strip } from "../lib/strips";
import { ALL_STRIPS, BANKS, eqBandsOf, sectionsOf, sendsOf, sourceLabel, stripAddrs, stripByPath } from "../lib/strips";
import type { Send } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useLeaf, useNodes, useSchema, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { FieldRow, Fields, NotConnected, Section } from "./common";

export function ChannelPage({ path, onSelect }: { path?: string; onSelect(path: string): void }) {
  const schema = useSchema();
  const strip = (path && stripByPath(path)) || ALL_STRIPS[0];
  if (!schema) return null;
  const i = ALL_STRIPS.indexOf(strip);
  const prev = ALL_STRIPS[(i + ALL_STRIPS.length - 1) % ALL_STRIPS.length];
  const next = ALL_STRIPS[(i + 1) % ALL_STRIPS.length];
  const a = stripAddrs(schema, strip);
  const sections = sectionsOf(schema, strip);
  const p = strip.path;

  return (
    <div className="page channel">
      <div className="channel-head">
        <button type="button" className="btn" onClick={() => onSelect(prev.path)} aria-label={`Previous: ${prev.label}`}>
          ‹
        </button>
        <Scribble strip={strip} nameAddr={a.name} colorAddr={a.color} large />
        <select className="select" value={strip.path} onChange={(e) => onSelect(e.target.value)} aria-label="Strip">
          {BANKS.map((b) => (
            <optgroup key={b.id} label={b.label}>
              {b.strips.map((s) => (
                <StripOption key={s.path} strip={s} />
              ))}
            </optgroup>
          ))}
        </select>
        <button type="button" className="btn" onClick={() => onSelect(next.path)} aria-label={`Next: ${next.label}`}>
          ›
        </button>
        {a.on && (
          <span className="channel-mute">
            <ParamControl addr={a.on} mute />
          </span>
        )}
      </div>
      <NotConnected />

      <div className="sections">
        {sections.includes("config") && (
          <Section title="Config">
            <div className="fields">
              <FieldRow addr={`${p}/config/name`} />
              <FieldRow addr={`${p}/config/color`} />
              <FieldRow addr={`${p}/config/icon`} />
              <SourceRow addr={`${p}/config/source`} />
            </div>
          </Section>
        )}
        {sections.includes("preamp") && (
          <Section title="Preamp">
            <Headamp strip={p} />
            <Fields path={`${p}/preamp`} />
          </Section>
        )}
        {sections.includes("delay") && (
          <Section title="Delay" on={`${p}/delay/on`}>
            <Fields path={`${p}/delay`} skip={["on"]} />
          </Section>
        )}
        {sections.includes("gate") && (
          <Section title="Gate" on={`${p}/gate/on`}>
            <Fields path={`${p}/gate`} skip={["on"]} />
            {schema.byPath.has(`${p}/gate/filter`) && (
              <>
                <h4>Key filter</h4>
                <Fields path={`${p}/gate/filter`} />
              </>
            )}
          </Section>
        )}
        {sections.includes("dyn") && (
          <Section title="Dynamics" on={`${p}/dyn/on`}>
            <Fields path={`${p}/dyn`} skip={["on"]} />
            {schema.byPath.has(`${p}/dyn/filter`) && (
              <>
                <h4>Key filter</h4>
                <Fields path={`${p}/dyn/filter`} />
              </>
            )}
          </Section>
        )}
        {sections.includes("eq") && <EqSection strip={strip} />}
        {sections.includes("insert") && (
          <Section title="Insert" on={`${p}/insert/on`}>
            <Fields path={`${p}/insert`} skip={["on"]} />
          </Section>
        )}
        {sections.includes("main") && (
          <Section title={strip.kind === "dca" ? "DCA" : "Main"}>
            {strip.kind === "dca" ? (
              <div className="fields">
                <FieldRow addr={`${p}/on`} label="Mute" mute />
                <FieldRow addr={`${p}/fader`} label="Fader" />
              </div>
            ) : (
              <div className="fields">
                <FieldRow addr={`${p}/mix/on`} label="Mute" mute />
                <Fields path={`${p}/mix`} skip={["on"]} />
              </div>
            )}
          </Section>
        )}
        {sections.includes("sends") && <SendsSection strip={strip} />}
        {sections.includes("groups") && (
          <Section title="Groups">
            <Fields path={`${p}/grp`} />
          </Section>
        )}
        {sections.includes("automix") && (
          <Section title="Automix">
            <Fields path={`${p}/automix`} />
          </Section>
        )}
      </div>
    </div>
  );
}

function StripOption({ strip }: { strip: Strip }) {
  const name = useValue(`${strip.path}/config/name`);
  const n = prettyText(name?.text);
  return <option value={strip.path}>{n ? `${strip.label} · ${n}` : strip.label}</option>;
}

function SourceRow({ addr }: { addr: string }) {
  const leaf = useLeaf(addr);
  const val = useValue(addr);
  useNodes(["/config/routing", "/config/routing/IN", "/config/routing/PLAY"]);
  if (!leaf) return null;
  const n = Number(val?.value);
  // channel inputs 1-32 go through the input routing: say where they come from
  const port = val && addr.startsWith("/ch/") && n >= 1 && n <= 32 ? inputSource(lookup, n) : null;
  return (
    <div className="field">
      <span className="field-label">Source</span>
      <span className="field-inline">
        <ParamControl addr={addr} />
        <span className="muted">
          {val ? sourceLabel(n) : ""}
          {port && ` · ${portLabel(port)}`}
        </span>
      </span>
    </div>
  );
}

function EqSection({ strip }: { strip: Strip }) {
  const schema = useSchema()!;
  const bands = eqBandsOf(schema, strip);
  const on = useValue(`${strip.path}/eq/on`);
  return (
    <Section title="EQ" on={`${strip.path}/eq/on`} className="eq">
      <EqCurve strip={strip.path} bands={bands} eqOn={on ? Number(on.value) === 1 : true} />
      <div className="bands">
        {bands.map((b, i) => (
          <div key={b} className="band">
            <h4>Band {i + 1}</h4>
            <Fields path={b} only={["type", "f", "g", "q"]} />
          </div>
        ))}
      </div>
    </Section>
  );
}

function SendsSection({ strip }: { strip: Strip }) {
  const schema = useSchema()!;
  const sends = sendsOf(schema, strip);
  const toMatrix = sends[0]?.target.path.startsWith("/mtx");
  return (
    <Section title={toMatrix ? "Matrix sends" : "Bus sends"} className="sends">
      <div className="sends-table" role="table">
        <div className="send-row head" role="row">
          <span role="columnheader">{toMatrix ? "Matrix" : "Bus"}</span>
          <span role="columnheader">On</span>
          <span role="columnheader">Level</span>
          <span role="columnheader">Pan</span>
          <span role="columnheader">Tap</span>
        </div>
        {sends.map((s) => (
          <SendRow key={s.path} send={s} />
        ))}
      </div>
    </Section>
  );
}

function SendRow({ send }: { send: Send }) {
  const name = useValue(`${send.target.path}/config/name`);
  const n = prettyText(name?.text);
  const hasPan = !!useLeaf(`${send.path}/pan`);
  const hasType = !!useLeaf(`${send.path}/type`);
  return (
    <div className="send-row" role="row">
      <span className="send-name" role="cell">
        <strong>{n || send.target.label}</strong>
        {n && <span className="muted"> {send.target.label}</span>}
      </span>
      <span role="cell">
        <ParamControl addr={`${send.path}/on`} />
      </span>
      <span role="cell" className="send-level">
        <ParamControl addr={`${send.path}/level`} />
      </span>
      <span role="cell" className="send-pan">
        {hasPan ? <ParamControl addr={`${send.path}/pan`} /> : <span className="muted">—</span>}
      </span>
      <span role="cell">{hasType ? <ParamControl addr={`${send.path}/type`} /> : <span className="muted">—</span>}</span>
    </div>
  );
}
