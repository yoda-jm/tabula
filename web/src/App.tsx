import { useEffect, useMemo } from "react";
import { ArmControl, ConnectionPill, RecIndicator, Toasts } from "./components/shell";
import { atLeast } from "./lib/access";
import type { Role } from "./lib/api";
import type { Page } from "./lib/route";
import { go, useHashRoute, usePersistent } from "./lib/route";
import { stripByPath } from "./lib/strips";
import { prettyText } from "./lib/values";
import { AccessPage } from "./pages/AccessPage";
import { BrowserPage } from "./pages/BrowserPage";
import { ChannelPage } from "./pages/ChannelPage";
import { ConnectPage } from "./pages/ConnectPage";
import { MixPage, MusicianApp, NoAccess } from "./pages/MixPage";
import { MixerPage } from "./pages/MixerPage";
import { RecordPage } from "./pages/RecordPage";
import { useAccess, useSchema, useStatus, useValue } from "./store/hooks";

/** The pages, with the least role that sees them. */
const NAV: { page: Page; label: string; role: Role }[] = [
  { page: "mixer", label: "Mixer", role: "viewer" },
  { page: "channel", label: "Channel", role: "viewer" },
  { page: "browser", label: "Browser", role: "viewer" },
  { page: "mix", label: "In-ears", role: "engineer" },
  { page: "record", label: "Record", role: "engineer" },
  { page: "access", label: "Access", role: "engineer" },
  { page: "connect", label: "Connect", role: "engineer" },
];

export function App() {
  const who = useAccess();
  if (!who) return <p className="banner">Reaching tabula…</p>;
  if (who.role === "none") return <NoAccess />;
  if (who.role === "musician") return <MusicianApp />;
  return <ConsoleApp role={who.role} />;
}

function ConsoleApp({ role }: { role: Role }) {
  const route = useHashRoute();
  const status = useStatus();
  const schema = useSchema();
  const [selected, setSelected] = usePersistent("tabula.strip", "/ch/01");
  const engineer = role === "engineer";
  const nav = useMemo(() => NAV.filter((n) => atLeast(role, n.role)), [role]);

  // no route yet (or a page this role does not see): the mixer when
  // connected, else the connection page
  useEffect(() => {
    if (!route || !nav.some((n) => n.page === route.page)) {
      go({ page: !engineer || status.connected || status.connecting ? "mixer" : "connect" });
    }
  }, [route, nav, engineer, status.connected, status.connecting]);

  // the Channel page's route carries the strip; keep the selection in sync
  useEffect(() => {
    if (route?.page === "channel" && route.strip && stripByPath(route.strip) && route.strip !== selected) setSelected(route.strip);
  }, [route, selected, setSelected]);

  const page = route?.page ?? (engineer ? "connect" : "mixer");
  const select = (path: string) => {
    setSelected(path);
    if (page === "channel") go({ page: "channel", strip: path });
  };
  const strip = stripByPath(selected) ?? stripByPath("/ch/01")!;

  return (
    <div className="app">
      <div className="header">
        <header className="topbar">
          <a className="brand" href="#/mixer" aria-label="tabula: mixer">
            tabula
          </a>
          <ConnectionPill onClick={() => engineer && go({ page: "connect" })} />
          <span className="topbar-gap" />
          <RecIndicator onClick={() => engineer && go({ page: "record" })} />
          {engineer ? <ArmControl /> : <span className="pill readonly">read-only</span>}
        </header>
        <nav className="nav" aria-label="Pages">
          {nav.map((n) => (
            <a
              key={n.page}
              href={n.page === "channel" ? `#/channel${strip.path}` : `#/${n.page}`}
              className={"nav-item" + (page === n.page ? " active" : "")}
              aria-current={page === n.page ? "page" : undefined}
            >
              {n.label}
              {n.page === "channel" && <SelectedName path={strip.path} label={strip.label} />}
            </a>
          ))}
        </nav>
      </div>
      <main className="main">
        {schema === null && page !== "connect" ? (
          <p className="banner">Loading the parameter schema…</p>
        ) : page === "mixer" ? (
          <MixerPage selected={strip.path} onSelect={select} />
        ) : page === "channel" ? (
          <ChannelPage path={route?.strip ?? strip.path} onSelect={select} />
        ) : page === "browser" ? (
          <BrowserPage />
        ) : page === "record" && engineer ? (
          <RecordPage />
        ) : page === "mix" && engineer ? (
          <MixPage bus={route?.bus} />
        ) : page === "access" && engineer ? (
          <AccessPage />
        ) : engineer ? (
          <ConnectPage />
        ) : (
          <MixerPage selected={strip.path} onSelect={select} />
        )}
      </main>
      <Toasts />
    </div>
  );
}

function SelectedName({ path, label }: { path: string; label: string }) {
  const name = useValue(`${path}/config/name`);
  const n = prettyText(name?.text);
  return <span className="nav-sub">{n || label}</span>;
}
