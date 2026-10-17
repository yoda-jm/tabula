// Routing: where the channel inputs come from, what the outputs carry, and
// what each output port (AES50 A and B, card, local XLR) sends, with sources
// followed to the strips and names they carry. Edited with the console's own
// routing blocks; everything stays read-only until writes are armed.

import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { IntSelect } from "../components/IntSelect";
import type { IntOption } from "../components/IntSelect";
import { ParamControl } from "../components/controls";
import { usePersistent } from "../lib/route";
import type { Port, PortSource } from "../lib/routing";
import {
  INPUT_BLOCKS,
  OUTPUT_SOURCES,
  auxBlockSource,
  blockLabel,
  describeSource,
  headampOfPort,
  headampPath,
  inputRouting,
  inputSource,
  outputDestinations,
  outputSource,
  portById,
  portChannelSource,
  portLabel,
  stripName,
  stripsUsingSource,
  userSource,
} from "../lib/routing";
import { prettyText } from "../lib/values";
import { useNodes, useSchema, useValue } from "../store/hooks";
import { lookup } from "../store/lookup";
import { NotConnected } from "./common";
import { range, two } from "../lib/num";

type Tab = "inputs" | "outputs" | "AES50A" | "AES50B" | "CARD" | "other";

const TABS: { id: Tab; label: string }[] = [
  { id: "inputs", label: "Inputs" },
  { id: "outputs", label: "Outputs 1-16" },
  { id: "AES50A", label: "AES50 A" },
  { id: "AES50B", label: "AES50 B" },
  { id: "CARD", label: "Card" },
  { id: "other", label: "Aux · P16 · AES · Rec" },
];

/** The nodes the routing views read: re-render when any of them changes. */
const WATCHED =
  /^\/(config\/routing|config\/userrout|outputs\/|-ha\/|-stat\/aes50$|(ch|auxin|fxrtn|bus|mtx)\/\d\d\/config$|main\/(st|m)\/config$)/;

export function RoutingPage() {
  const schema = useSchema();
  const [tab, setTab] = usePersistent<Tab>("tabula.routing", "inputs");
  const paths = useMemo(() => (schema ? schema.nodes.map((n) => n.path).filter((p) => WATCHED.test(p)) : []), [schema]);
  const version = useNodes(paths);
  const sourceOptions = useMemo(() => outputSourceOptions(), [version]);
  if (!schema) return null;
  const current = TABS.find((t) => t.id === tab) ?? TABS[0];
  return (
    <div className="page routing">
      <div className="banks" role="tablist" aria-label="Routing views">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={t.id === current.id}
            className={"bank" + (t.id === current.id ? " active" : "")}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <NotConnected />
      <div role="tabpanel" aria-label={current.label}>
        {current.id === "inputs" && <InputsView />}
        {current.id === "outputs" && <OutputsView options={sourceOptions} />}
        {(current.id === "AES50A" || current.id === "AES50B" || current.id === "CARD") && (
          <PortView port={portById(current.id)} />
        )}
        {current.id === "other" && <OtherView options={sourceOptions} />}
      </div>
    </div>
  );
}

/** outputSourceOptions lists the 77 output sources with the names of their strips. */
function outputSourceOptions(): IntOption[] {
  return range(0, OUTPUT_SOURCES - 1).map((n) => {
    const o = outputSource(n);
    const name = stripName(lookup, o.strip);
    return { value: n, label: name ? `${o.label} · ${name}` : o.label };
  });
}

function Card({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="card">
      <header className="section-head">
        <h3>{title}</h3>
        {aside}
      </header>
      {children}
    </section>
  );
}

// --- inputs ---

function InputsView() {
  const active = inputRouting(lookup);
  return (
    <>
      <Card title="Input routing" aside={<ParamControl addr="/config/routing/routswitch" />}>
        <p className="muted route-hint">
          REC takes the inputs from the live blocks; PLAY from the playback blocks (usually the card, for a virtual soundcheck).
          The switch is the console's REC/PLAY routing switch.
        </p>
        <div className="route-columns">
          {(["/config/routing/IN", "/config/routing/PLAY"] as const).map((node) => (
            <div key={node} className={"route-col" + (node === active ? " active" : "")}>
              <h4>
                {node.endsWith("IN") ? "Live (REC)" : "Playback (PLAY)"}
                {node === active && <span className="badge">in use</span>}
              </h4>
              {INPUT_BLOCKS.map((b) => (
                <BlockRow key={b} label={`In ${b}`} addr={`${node}/${b}`} />
              ))}
              <BlockRow label="Aux in" addr={`${node}/AUX`} />
            </div>
          ))}
        </div>
      </Card>
      <Card title="Input patch">
        <p className="muted route-hint">Where each console input comes from, its headamp and the channels using it.</p>
        <div className="route-table" role="table" aria-label="Input patch">
          <div className="route-row patch head" role="row">
            <span role="columnheader">Input</span>
            <span role="columnheader">From</span>
            <span role="columnheader">Headamp</span>
            <span role="columnheader">Channels</span>
          </div>
          {range(1, 32).map((n) => (
            <PatchRow key={n} label={`In ${two(n)}`} port={inputSource(lookup, n)} source={n} />
          ))}
          {range(1, 6).map((n) => (
            <PatchRow
              key={"aux" + n}
              label={`Aux ${n}`}
              port={auxBlockSource(lookup.text(`${active}/AUX`)?.trim() ?? "", n)}
              source={32 + n}
            />
          ))}
        </div>
      </Card>
    </>
  );
}

/** BlockRow edits one routing block and says what it is. */
function BlockRow({ label, addr }: { label: string; addr: string }) {
  const v = useValue(addr);
  const text = prettyText(v?.text);
  return (
    <div className="block-row">
      <span className="block-label">{label}</span>
      <ParamControl addr={addr} />
      <span className="muted block-desc">{text ? blockLabel(text) : ""}</span>
    </div>
  );
}

function PatchRow({ label, port, source }: { label: string; port: PortSource | null; source: number }) {
  const channels = stripsUsingSource(lookup, source);
  const ha = port ? headampOfPort(port) : -1;
  return (
    <div className={"route-row patch" + (channels.length ? "" : " unused")} role="row">
      <span role="cell" className="route-label">
        {label}
      </span>
      <span role="cell" className="mono">
        {port ? portLabel(port) : "OFF"}
      </span>
      <span role="cell">{ha >= 0 ? <HeadampText index={ha} /> : <span className="muted">—</span>}</span>
      <span role="cell" className="route-names">
        {channels.length ? channels.join(", ") : <span className="muted">not used</span>}
      </span>
    </div>
  );
}

function HeadampText({ index }: { index: number }) {
  const gain = useValue(`${headampPath(index)}/gain`);
  const phantom = useValue(`${headampPath(index)}/phantom`);
  return (
    <span className="mono">
      {gain ? prettyText(gain.text) + " dB" : "—"}
      {Number(phantom?.value) === 1 && <span className="badge phantom">48 V</span>}
    </span>
  );
}

// --- outputs 1-16 ---

function OutputsView({ options }: { options: IntOption[] }) {
  const dest = outputDestinations(lookup);
  return (
    <Card title="Outputs 1-16">
      <p className="muted route-hint">
        The console's outputs. Each output port (AES50, card, local XLR) picks them in blocks: the last column says where each one
        goes.
      </p>
      <div className="route-table" role="table" aria-label="Outputs 1-16">
        <div className="route-row out head" role="row">
          <span role="columnheader">Output</span>
          <span role="columnheader">Source</span>
          <span role="columnheader">Tap</span>
          <span role="columnheader">Invert</span>
          <span role="columnheader">Delay</span>
          <span role="columnheader">Goes to</span>
        </div>
        {range(1, 16).map((n) => (
          <OutputRow
            key={n}
            node={`/outputs/main/${two(n)}`}
            label={`Out ${two(n)}`}
            options={options}
            delay
            dest={dest[n - 1]}
          />
        ))}
      </div>
    </Card>
  );
}

function OutputRow(props: {
  node: string;
  label: string;
  options: IntOption[];
  delay?: boolean;
  dest?: string[];
  noInvert?: boolean;
}) {
  const { node } = props;
  return (
    <div className="route-row out" role="row">
      <span role="cell" className="route-label">
        {props.label}
      </span>
      <span role="cell" className="route-src">
        <IntSelect addr={`${node}/src`} options={props.options} label={`${props.label} source`} />
      </span>
      <span role="cell">
        <ParamControl addr={`${node}/pos`} />
      </span>
      <span role="cell">{props.noInvert ? <span className="muted">—</span> : <ParamControl addr={`${node}/invert`} />}</span>
      <span role="cell" className="route-delay">
        {props.delay ? (
          <>
            <ParamControl addr={`${node}/delay/on`} />
            <ParamControl addr={`${node}/delay/time`} />
          </>
        ) : (
          <span className="muted">—</span>
        )}
      </span>
      <span role="cell" className="route-names">
        {props.dest === undefined ? null : props.dest.length ? props.dest.join(", ") : <span className="muted">nowhere</span>}
      </span>
    </div>
  );
}

// --- AES50 A and B, card ---

function PortView({ port }: { port: Port }) {
  const device = useValue(port.id === "AES50A" ? "/-stat/aes50/A" : port.id === "AES50B" ? "/-stat/aes50/B" : undefined);
  const deviceName = prettyText(device?.text);
  const aes = port.id !== "CARD";
  return (
    <>
      <Card title={`${port.label} outputs`} aside={deviceName ? <span className="badge">{deviceName}</span> : undefined}>
        <p className="muted route-hint">
          {aes
            ? `What the console sends on ${port.label}, in blocks of 8. A stage box plays these channels on its outputs: on a DL32, output N is channel N (1-16).`
            : "What the console sends to the expansion card (the tracks a computer records over USB), in blocks of 8."}
        </p>
        <div className="blocks">
          {port.blocks.map((b) => (
            <BlockRow key={b} label={b} addr={`${port.node}/${b}`} />
          ))}
        </div>
      </Card>
      <Card title={aes ? `${port.label} channels` : "Card tracks"}>
        <PortChannels port={port} />
      </Card>
    </>
  );
}

function PortChannels({ port }: { port: Port }) {
  const aes = port.id === "AES50A" || port.id === "AES50B";
  const rows = range(1, port.channels).map((n) => ({ n, src: portChannelSource(lookup, port, n) }));
  return (
    <div className="route-table" role="table" aria-label={`${port.label} channels`}>
      {rows.map(({ n, src }) => (
        <div key={n} className={"route-row chan" + (src ? "" : " unused") + (aes && n === 17 ? " gap" : "")} role="row">
          <span role="cell" className="route-label">
            {port.id === "OUT" ? `XLR ${n}` : port.id === "CARD" ? `Track ${n}` : `${port.label.slice(-1)} ${n}`}
            {aes && n <= 16 && <span className="muted route-sub">box out {n}</span>}
          </span>
          <span role="cell" className="route-names">
            {describeSource(lookup, src)}
          </span>
        </div>
      ))}
    </div>
  );
}

// --- aux, P16, AES/EBU, rec outputs, local outputs, user routing ---

function OtherView({ options }: { options: IntOption[] }) {
  const local = portById("OUT");
  return (
    <>
      <OutputGroup title="Aux outputs" node="/outputs/aux" count={6} label="Aux" options={options} />
      <OutputGroup title="P16 (Ultranet)" node="/outputs/p16" count={16} label="P16" options={options} />
      <OutputGroup title="AES/EBU" node="/outputs/aes" count={2} label="AES" options={options} />
      <OutputGroup title="2-track recording" node="/outputs/rec" count={2} label="Rec" options={options} noInvert />
      <Card title="Local XLR outputs">
        <p className="muted route-hint">The console's own output sockets, in blocks of 4 (consoles without them ignore this).</p>
        <div className="blocks">
          {local.blocks.map((b) => (
            <BlockRow key={b} label={b} addr={`${local.node}/${b}`} />
          ))}
        </div>
        <PortChannels port={local} />
      </Card>
      <UserRouting />
    </>
  );
}

function OutputGroup(props: {
  title: string;
  node: string;
  count: number;
  label: string;
  options: IntOption[];
  noInvert?: boolean;
}) {
  return (
    <Card title={props.title}>
      <div className="route-table" role="table" aria-label={props.title}>
        {range(1, props.count).map((n) => (
          <OutputRow
            key={n}
            node={`${props.node}/${two(n)}`}
            label={`${props.label} ${n}`}
            options={props.options}
            noInvert={props.noInvert}
          />
        ))}
      </div>
    </Card>
  );
}

/** userOptions lists the 209 user routing values. */
function userOptions(): IntOption[] {
  return range(0, 208).map((n) => {
    const p = userSource(n);
    return { value: n, label: p ? portLabel(p) : "OFF" };
  });
}

function UserRouting() {
  const [open, setOpen] = useState(false);
  const used = [
    ...range(1, 32)
      .filter((n) => Number(lookup.value(`/config/userrout/in/${two(n)}`) ?? 0) > 0)
      .map((n) => `in ${n}`),
    ...range(1, 48)
      .filter((n) => Number(lookup.value(`/config/userrout/out/${two(n)}`) ?? 0) > 0)
      .map((n) => `out ${n}`),
  ];
  const options = useMemo(userOptions, []);
  return (
    <Card
      title="User routing"
      aside={
        <button type="button" className="btn" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? "Hide" : "Edit"}
        </button>
      }
    >
      <p className="muted route-hint">
        User inputs (UIN blocks) and user outputs (UOUT blocks) patch single channels.{" "}
        {used.length ? `In use: user ${used.join(", ")}.` : "None is patched."}
      </p>
      {open && (
        <div className="user-rout">
          <h4>User inputs</h4>
          <div className="user-grid">
            {range(1, 32).map((n) => (
              <label key={n} className="user-cell">
                <span className="field-label">User in {n}</span>
                <IntSelect addr={`/config/userrout/in/${two(n)}`} options={options.slice(0, 169)} label={`User in ${n}`} />
              </label>
            ))}
          </div>
          <h4>User outputs</h4>
          <div className="user-grid">
            {range(1, 48).map((n) => (
              <label key={n} className="user-cell">
                <span className="field-label">User out {n}</span>
                <IntSelect addr={`/config/userrout/out/${two(n)}`} options={options} label={`User out ${n}`} />
              </label>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}
