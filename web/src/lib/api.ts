// Types and calls of the tabula HTTP API (docs/API.md).

export interface ConsoleInfo {
  server_version: string;
  server_name: string;
  model: string;
  firmware: string;
  ip: string;
  name: string;
}

export interface ConsoleStatus {
  addr: string;
  info: ConsoleInfo;
  connected: boolean;
  synced: number;
  total: number;
  missing?: string[];
  armed: boolean;
  last_seen: string;
  /** the link's quality, measured by reading the console back */
  link?: Link;
}

/** Link is the quality of the link to the console (lib/link.ts reads it). */
export interface Link {
  /** smoothed round trip of a request */
  rtt_ms: number;
  /** requests measured (the last ones, up to 100), and how many were left unanswered */
  asked: number;
  lost: number;
  /** values found changed since the connection: changes lost on the network */
  caught: number;
  last_caught?: string;
  caught_at?: string;
}

export interface Status {
  connected: boolean;
  connecting: boolean;
  error?: string;
  console?: ConsoleStatus;
  /** tabula's version ("v0.2.0", "dev 9f91019") */
  version?: string;
}

/** About is which tabula runs, since when, and where it keeps its files (GET /api/about, the engineer's). */
export interface About {
  version: string;
  started: string;
  log: string; // "": the terminal only
  sessions: string;
  scenes: string;
  access: string;
  listen: string;
}

export type Kind = "enum" | "int" | "bitmap" | "string" | "lin" | "log" | "level" | "unused" | "raw";

export interface TypeInfo {
  kind: Kind;
  describe: string;
  names?: string[];
  min?: number;
  max?: number;
  steps?: number;
  bits?: number;
  unit?: string;
}

export interface SchemaField {
  name: string;
  label?: string;
  type: number;
  type_info?: TypeInfo;
}

export interface SchemaNode {
  path: string;
  scene: boolean;
  variant?: string;
  fields: SchemaField[] | null;
}

export interface SchemaResponse {
  model: string;
  types: TypeInfo[];
  nodes: SchemaNode[];
}

export type RawValue = number | string;

export interface ValueView {
  addr: string;
  text: string;
  value: RawValue;
}

export interface StateNode {
  path: string;
  line: string;
  values: ValueView[] | null;
  fields?: SchemaField[];
  text?: boolean;
}

export interface Change extends ValueView {
  /** "console", "local" (this tabula's engineer) or "web <name>" (an access link) */
  origin: string;
}

export type Role = "none" | "viewer" | "musician" | "engineer";

/** Who the browser is for tabula (/api/whoami). */
export interface Whoami {
  role: Role;
  name?: string;
  /** a musician's mix: their bus, and its linked partner for a stereo mix */
  bus?: number;
  buses?: number[];
  local: boolean;
  /** signed in with an access link */
  link: boolean;
  /** signed in with the engineer password */
  session?: boolean;
  /** an engineer password is set: this machine signs in too */
  password?: boolean;
  musicians_can_write: boolean;
}

export interface Grant {
  id: string;
  name: string;
  role: Role;
  bus?: number;
  /** a device signed in with the engineer password (its token is not shown) */
  session?: boolean;
  token: string;
  created: string;
}

/** Presence tells whether an access is in use: a page open with it keeps an event stream open. */
export interface Presence {
  /** event streams open now; 0 is offline */
  streams: number;
  /** the IP address of its last stream */
  addr?: string;
  /** when it last opened or closed one */
  seen: string;
}

/** Online tells which devices have a page open (also the "online" event, sent to engineers). */
export interface Online {
  /** by access; one not opened since tabula started is left out */
  grants: Record<string, Presence>;
  /** the addresses of devices open without an access */
  guests: string[];
}

export interface AccessStatus {
  /** access links and signed-in devices */
  grants: Grant[];
  musicians_can_write: boolean;
  /** an engineer password is set */
  password: boolean;
  /** the access of this browser, if any */
  current?: string;
  guest: Role;
  /** where other devices reach this tabula */
  urls: string[];
  /** the same with this machine's name ("http://actarus.local:8033"), when the network answers for it */
  name_url?: string;
  /** false when tabula listens on this machine only */
  reachable: boolean;
  /** which devices have a page open */
  online: Online;
}

export interface Discovered {
  ip: string;
  name: string;
  model: string;
  firmware: string;
}

export type SetBody = { addr: string; value: RawValue } | { addr: string; text: string };

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

/** TIMEOUT is how long a request waits for its answer: on a connection gone silent it fails instead of hanging. */
export const TIMEOUT = 10_000;
/** LONG is for the calls that wait on purpose: an audio test, a recording that starts or ends, the USB stick… */
export const LONG = 60_000;
/** SLOW is for the big answers (the schema, the whole state) over a weak Wi-Fi. */
export const SLOW = 30_000;

/** call sends a request; it fails after timeout ms without an answer (0: it waits as long as it takes). */
export async function call<T>(method: "GET" | "POST" | "PUT" | "DELETE", url: string, body?: unknown, timeout = TIMEOUT): Promise<T> {
  const ctl = timeout > 0 ? new AbortController() : null;
  const timer = ctl ? setTimeout(() => ctl.abort(), timeout) : undefined;
  try {
    const res = await fetch(url, {
      method,
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: ctl?.signal,
    });
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`;
      try {
        const j = await res.json();
        if (j && typeof j.error === "string") msg = j.error;
      } catch {
        // not JSON: keep the status line
      }
      throw new ApiError(res.status, msg);
    }
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  } catch (e) {
    if (ctl?.signal.aborted) throw new Error(`tabula did not answer within ${timeout / 1000} s`);
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

export const api = {
  status: () => call<Status>("GET", "/api/status"),
  about: () => call<About>("GET", "/api/about"),
  discover: () => call<Discovered[]>("GET", "/api/discover"),
  connect: (addr: string) => call<Status>("POST", "/api/connect", { addr }),
  disconnect: () => call<Status>("POST", "/api/disconnect", {}),
  arm: (armed: boolean) => call<Status>("POST", "/api/arm", { armed }),
  schema: () => call<SchemaResponse>("GET", "/api/schema", undefined, SLOW),
  state: (prefix?: string) =>
    call<StateNode[]>("GET", "/api/state" + (prefix ? "?prefix=" + encodeURIComponent(prefix) : ""), undefined, SLOW),
  set: (body: SetBody) => call<void>("POST", "/api/set", body),
  whoami: () => call<Whoami>("GET", "/api/whoami"),
  logout: () => call<Whoami>("POST", "/api/logout", {}),
  login: (password: string) => call<Whoami>("POST", "/api/login", { password }),
};

/** UndoView is what Undo would undo: the engineer's last change made from tabula (GET /api/undo, "undo" events). */
export interface UndoView {
  count: number;
  /** "/ch/12/mix/fader (was -10.0)", "copy of /ch/01 (eq) to /ch/02" */
  what?: string;
  at?: string;
}

/** The sections of a strip that can be copied to others (POST /api/strips/copy). */
export type CopySection = "preamp" | "delay" | "gate" | "dyn" | "eq" | "insert" | "sends" | "main";

export const editApi = {
  undoStatus: () => call<UndoView>("GET", "/api/undo"),
  undo: () => call<UndoView>("POST", "/api/undo", {}),
  copyStrip: (from: string, to: string[], sections: CopySection[]) =>
    call<{ nodes: number }>("POST", "/api/strips/copy", { from, to, sections }),
};

export const accessApi = {
  list: () => call<AccessStatus>("GET", "/api/access"),
  add: (name: string, role: Role, bus?: number) => call<Grant>("POST", "/api/access", { name, role, bus }),
  remove: (id: string) => call<void>("DELETE", "/api/access/" + encodeURIComponent(id)),
  settings: (musicians_can_write: boolean) => call<AccessStatus>("POST", "/api/access/settings", { musicians_can_write }),
  /** password sets, changes or (empty) removes the engineer password; current is needed when one is set */
  password: (current: string, password: string) => call<AccessStatus>("POST", "/api/access/password", { current, password }),
};
