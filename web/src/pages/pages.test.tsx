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
import { MixPage, MusicianApp } from "./MixPage";
import { MonitorPage } from "./MonitorPage";
import { NoAccess } from "./NoAccess";
import { RecordPage } from "./RecordPage";
import { RoutingPage } from "./RoutingPage";
import { SceneFiles } from "./SceneFiles";
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
    render(<ScenesPage />);
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

  it("every in-ear mix", () => {
    for (let bus = 1; bus <= 16; bus++) render(<MixPage bus={bus} />);
  }, 60_000);

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
