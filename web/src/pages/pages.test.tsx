// Every page rendered from what tabula serves for the demo show on an
// emulated M32C (written by `go run ./web/fixture`, so Go is needed): a page
// that throws, prints NaN or undefined, or makes React warn (a list without
// keys…) fails here. Server rendering: effects do not run, so this covers
// what a page shows from the store, not its clicks.

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import type { ReactElement } from "react";
import { renderToString } from "react-dom/server";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { Status, Whoami } from "../lib/api";
import { UpdateBanner } from "../components/shell";
import { ALL_STRIPS } from "../lib/strips";
import { store } from "../store/store";
import { AccessPage } from "./AccessPage";
import { BrowserPage } from "./BrowserPage";
import { ChannelPage } from "./ChannelPage";
import { ConnectPage } from "./ConnectPage";
import { FxPage } from "./FxPage";
import { MixerPage } from "./MixerPage";
import { MetersPage } from "./MetersPage";
import { MixPage, MusicianApp } from "./MixPage";
import { MonitorPage } from "./MonitorPage";
import { NoAccess } from "./NoAccess";
import { RecordPage } from "./RecordPage";
import { RoutingPage } from "./RoutingPage";
import { SceneSafes, SnippetFilters } from "./Safes";
import { SceneFiles } from "./SceneFiles";
import { TakeHome } from "./TakeHome";
import { ScenesPage } from "./ScenesPage";
import { UsbPage } from "./UsbPage";

const root = fileURLToPath(new URL("../../..", import.meta.url));
const engineer: Whoami = { role: "engineer", local: true, link: false, musicians_can_write: true };
const viewer: Whoami = { role: "viewer", local: false, link: true, musicians_can_write: true };
const musician: Whoami = { role: "musician", name: "Margot", bus: 3, buses: [3, 4], local: false, link: true, musicians_can_write: true };

let dir = "";
let status: Status;
let errors: string[] = [];

beforeAll(async () => {
  dir = mkdtempSync(join(tmpdir(), "tabula-pages-"));
  execFileSync("go", ["run", "./web/fixture", dir], { cwd: root, stdio: "inherit" });
  const json = (name: string) => JSON.parse(readFileSync(join(dir, name + ".json"), "utf8"));
  status = json("status");
  const answers: Record<string, unknown> = {
    "/api/whoami": engineer,
    "/api/schema": json("schema"),
    "/api/state": json("state"),
    "/api/status": status,
    "/api/record": json("record"),
  };
  vi.stubGlobal("fetch", async (url: string) => {
    const body = answers[url.split("?")[0]];
    return new Response(JSON.stringify(body ?? { error: "not in the fixture" }), {
      status: body === undefined ? 404 : 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  await store.loadAccess();
  await store.loadSchema();
  store.setStatus(status);
  store.setRecord(json("record"));
  await store.reloadState();
}, 120_000);

afterAll(() => {
  vi.unstubAllGlobals();
  if (dir) rmSync(dir, { recursive: true, force: true });
});

// React's warnings (keys, invalid DOM nesting…) fail the test that caused them
beforeEach(() => {
  errors = [];
  vi.spyOn(console, "error").mockImplementation((...args: unknown[]) => void errors.push(args.map(String).join(" ")));
  store.setAccess(engineer);
});
afterEach(() => {
  vi.restoreAllMocks();
  expect(errors).toEqual([]);
});

/** render renders a page and checks it shows no broken value. */
function render(page: ReactElement): string {
  const html = renderToString(page);
  expect(html).not.toMatch(/\bNaN\b|\bundefined\b|\[object Object\]|Infinity/); // in the text or an attribute (cx="NaN")
  return html;
}

const noop = () => {};

describe("pages", () => {
  it("loaded the demo show", () => {
    expect(store.status.connected).toBe(true);
    expect(store.getValue("/ch/12/config/name")?.value).toBe("Vx Theo");
  });

  it("mixer", () => {
    const html = render(<MixerPage selected="/ch/01" onSelect={noop} />);
    expect(html).toContain("Mute groups");
    expect(html).toContain("No solo");
  });

  it("every strip on the Channel page", () => {
    for (const s of ALL_STRIPS) {
      const html = render(<ChannelPage path={s.path} onSelect={noop} />);
      expect(html, s.path).toContain(s.label);
    }
    expect(render(<ChannelPage path="/ch/14" onSelect={noop} />)).toContain("eq-handle");
  }, 60_000);

  it("meter bridge, gain reductions and DCA meters", () => {
    // meters as the console sends them: levels, gate and compressor gains (1 = none), DCA levels
    store.setMeters("0", Array.from({ length: 70 }, (_, i) => (i === 11 ? 1.2 : 0.1)));
    store.setMeters("1", [...Array(32).fill(0.1), ...Array(32).fill(1), ...Array(32).fill(0.5)]);
    store.setMeters("5:0:0", Array(27).fill(0.2));
    try {
      const html = render(<MetersPage />);
      for (const title of ["Inputs", "Aux ins", "FX returns", "Mix buses", "Matrices", "Main", "DCAs"]) expect(html).toContain(title);
      expect(html).toContain("Vx Theo");
      expect(html).toContain("Gain reduction 6.0 dB"); // the channels' compressors at a gain of 0.5
      expect(html).toContain("Ch 12 clipped"); // above 0 dBFS: its clip light is lit
      const channel = render(<ChannelPage path="/ch/12" onSelect={noop} />);
      expect(channel).toContain("dyn-meter");
      expect(channel).toContain("6.0 dB");
      expect(render(<MixerPage selected="/ch/01" onSelect={noop} />)).toContain("meter gr");
    } finally {
      for (const id of ["0", "1", "5:0:0"]) store.setMeters(id, []);
    }
  });

  it("monitor", () => {
    const html = render(<MonitorPage />);
    expect(html).toContain("Talkback");
    expect(html).toContain("Oscillator");
  });

  it("routing", () => {
    expect(render(<RoutingPage />)).toContain("Patchbay");
  });

  it("every FX slot", () => {
    render(<FxPage />);
    for (let slot = 1; slot <= 8; slot++) render(<FxPage slot={slot} />);
  });

  it("scenes, scene files, USB stick", () => {
    expect(render(<ScenesPage />)).toContain("What every scene recall keeps");
    expect(render(<SceneSafes slot={1} />)).toContain("Routing");
    expect(render(<SnippetFilters slot={0} />)).toContain("Vx Theo"); // strips show their names
    render(<SceneFiles />);
    render(<UsbPage />);
  });

  it("browser", () => {
    expect(render(<BrowserPage />)).toContain("/ch");
  });

  it("record, connect, access", () => {
    const record = render(<RecordPage />);
    expect(record).toContain("Start recording");
    expect(record).toContain("USB stick");
    expect(render(<ConnectPage />)).toContain("tabula-emu");
    render(<AccessPage />);
  });

  it("taking a session home", () => {
    const detail = {
      name: "2026-10-25_203512",
      meta: { start: "2026-10-25T20:35:12Z", events: 120, markers: 3, duration: 5400,
        audio: { rate: 48000, bits: 24, channels: 32, offset: 0.1, frames: 2.6e8, tracks: [{ channel: 1, name: "Kick", file: "audio/01 Kick.wav" }] } },
      size: 12.3e9,
      flac_size: 8.6e9,
    };
    const html = render(<TakeHome detail={detail} recording={false} recordingThis={false} />);
    expect(html).toContain("Download as a zip");
    expect(html.replace(/<!-- -->/g, "")).toContain("FLAC · about 8.6 GB");
    expect(render(<TakeHome detail={detail} recording={true} recordingThis={true} />)).toContain("still being recorded");
  });

  it("every in-ear mix", () => {
    for (let bus = 1; bus <= 16; bus++) render(<MixPage bus={bus} />);
  }, 60_000);

  it("a stereo in-ear mix has pans", () => {
    const link = (on: number) => store.applyChanges([{ addr: "/config/buslink/7-8", text: on ? "ON" : "OFF", value: on, origin: "console" }]);
    link(1);
    try {
      const html = render(<MixPage bus={7} />);
      expect(html).toContain("Buses 7-8, stereo");
      expect(html).toContain("iem-pan-track");
    } finally {
      link(0);
    }
  });

  it("a musician's phone", () => {
    store.setAccess(musician);
    render(<MusicianApp />);
  });

  it("a viewer sees the pages read-only", () => {
    store.setAccess(viewer);
    const html = render(<MixerPage selected="/ch/01" onSelect={noop} />);
    expect(html).toContain("disabled");
    render(<ChannelPage path="/ch/14" onSelect={noop} />);
    render(<MonitorPage />);
  });

  it("a device without access", () => {
    store.setAccess({ role: "none", local: false, link: false, musicians_can_write: false });
    render(<NoAccess />);
  });

  it("asks to reload a page opened before tabula was updated", () => {
    expect(render(<UpdateBanner />)).toBe("");
    store.setStatus({ ...status, version: "v9.9.9" });
    expect(render(<UpdateBanner />)).toContain("reload the page");
    store.setStatus(status);
  });
});
