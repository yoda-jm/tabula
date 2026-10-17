import { useEffect } from "react";
import { ArmControl, ConnectionPill, RecIndicator, Toasts } from "./components/shell";
import type { Page } from "./lib/route";
import { go, useHashRoute, usePersistent } from "./lib/route";
import { stripByPath } from "./lib/strips";
import { prettyText } from "./lib/values";
import { BrowserPage } from "./pages/BrowserPage";
import { ChannelPage } from "./pages/ChannelPage";
import { ConnectPage } from "./pages/ConnectPage";
import { MixerPage } from "./pages/MixerPage";
import { RecordPage } from "./pages/RecordPage";
import { RoutingPage } from "./pages/RoutingPage";
import { useSchema, useStatus, useValue } from "./store/hooks";

const NAV: { page: Page; label: string }[] = [
  { page: "mixer", label: "Mixer" },
  { page: "channel", label: "Channel" },
  { page: "routing", label: "Routing" },
  { page: "browser", label: "Browser" },
  { page: "record", label: "Record" },
  { page: "connect", label: "Connect" },
];

export function App() {
  const route = useHashRoute();
  const status = useStatus();
  const schema = useSchema();
  const [selected, setSelected] = usePersistent("tabula.strip", "/ch/01");

  // no route yet: the mixer when connected, else the connection page
  useEffect(() => {
    if (!route) go({ page: status.connected || status.connecting ? "mixer" : "connect" });
  }, [route, status.connected, status.connecting]);

  // the Channel page's route carries the strip; keep the selection in sync
  useEffect(() => {
    if (route?.page === "channel" && route.strip && stripByPath(route.strip) && route.strip !== selected) setSelected(route.strip);
  }, [route, selected, setSelected]);

  const page = route?.page ?? "connect";
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
          <ConnectionPill onClick={() => go({ page: "connect" })} />
          <span className="topbar-gap" />
          <RecIndicator onClick={() => go({ page: "record" })} />
          <ArmControl />
        </header>
        <nav className="nav" aria-label="Pages">
          {NAV.map((n) => (
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
        ) : page === "routing" ? (
          <RoutingPage />
        ) : page === "browser" ? (
          <BrowserPage />
        ) : page === "record" ? (
          <RecordPage />
        ) : (
          <ConnectPage />
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
