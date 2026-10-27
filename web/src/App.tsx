import { useEffect, useMemo } from "react";
import { ArmControl, AudioBanner, ConnectionPill, RecIndicator, StreamBanner, Toasts, UndoButton, UpdateBanner } from "./components/shell";
import { atLeast } from "./lib/access";
import type { Role } from "./lib/api";
import { api } from "./lib/api";
import type { Page } from "./lib/route";
import { go, useHashRoute, usePersistent } from "./lib/route";
import { stripByPath } from "./lib/strips";
import { prettyText } from "./lib/values";
import { AccessPage } from "./pages/AccessPage";
import { BrowserPage } from "./pages/BrowserPage";
import { ChannelPage } from "./pages/ChannelPage";
import { ConnectPage } from "./pages/ConnectPage";
import { FxPage } from "./pages/FxPage";
import { MixPage, MusicianApp } from "./pages/MixPage";
import { MixerPage } from "./pages/MixerPage";
import { MonitorPage } from "./pages/MonitorPage";
import { NoAccess } from "./pages/NoAccess";
import { RecordPage } from "./pages/RecordPage";
import { RoutingPage } from "./pages/RoutingPage";
import { ScenesPage } from "./pages/ScenesPage";
import { UsbPage } from "./pages/UsbPage";
import { useAccess, useSchema, useStatus, useValue } from "./store/hooks";
import { store } from "./store/store";

/** The pages, with the least role that sees them. */
const NAV: { page: Page; label: string; role: Role }[] = [
  { page: "mixer", label: "Mixer", role: "viewer" },
  { page: "channel", label: "Channel", role: "viewer" },
  { page: "monitor", label: "Monitor", role: "engineer" },
  { page: "routing", label: "Routing", role: "engineer" },
  { page: "fx", label: "FX", role: "engineer" },
  { page: "scenes", label: "Scenes", role: "engineer" },
  { page: "usb", label: "USB", role: "engineer" },
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
  const who = useAccess();
  const [selected, setSelected] = usePersistent("tabula.strip", "/ch/01");
  const engineer = role === "engineer";
  const nav = useMemo(() => NAV.filter((n) => atLeast(role, n.role)), [role]);

  // no route yet (or a page this role does not see): the mixer when
  // connected, else the connection page, once the status is known
  useEffect(() => {
    if (engineer && !store.statusKnown) return;
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
          <a className="brand" href="#/mixer" aria-label="tabula: mixer" title={status.version ? `tabula ${status.version}` : undefined}>
            tabula
          </a>
          <ConnectionPill onClick={() => engineer && go({ page: "connect" })} />
          <span className="topbar-gap" />
          <RecIndicator onClick={() => engineer && go({ page: "record" })} />
          {engineer && <UndoButton />}
          {engineer ? <ArmControl /> : <span className="pill readonly">read-only</span>}
          {who?.session && <SignOut />}
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
        <StreamBanner />
        <UpdateBanner />
        {engineer && <AudioBanner onOpen={page !== "record" ? () => go({ page: "record" }) : undefined} />}
      </div>
      <main className="main">
        {schema === null && page !== "connect" ? (
          <p className="banner">Loading the parameter schema…</p>
        ) : page === "mixer" ? (
          <MixerPage selected={strip.path} onSelect={select} />
        ) : page === "channel" ? (
          <ChannelPage path={route?.strip ?? strip.path} onSelect={select} />
        ) : page === "monitor" && engineer ? (
          <MonitorPage />
        ) : page === "routing" && engineer ? (
          <RoutingPage />
        ) : page === "fx" && engineer ? (
          <FxPage slot={route?.slot} />
        ) : page === "scenes" && engineer ? (
          <ScenesPage />
        ) : page === "usb" && engineer ? (
          <UsbPage />
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

/** SignOut ends this browser's session (signed in with the engineer password). */
function SignOut() {
  const out = async () => {
    await api.logout().catch(() => undefined);
    window.location.reload();
  };
  return (
    <button type="button" className="btn" onClick={out} title="Sign out of tabula on this device">
      Sign out
    </button>
  );
}

function SelectedName({ path, label }: { path: string; label: string }) {
  const name = useValue(`${path}/config/name`);
  const n = prettyText(name?.text);
  return <span className="nav-sub">{n || label}</span>;
}
