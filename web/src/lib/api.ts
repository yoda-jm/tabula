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
}

export interface Status {
  connected: boolean;
  connecting: boolean;
  error?: string;
  console?: ConsoleStatus;
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
  musicians_can_write: boolean;
}

export interface Grant {
  id: string;
  name: string;
  role: Role;
  bus?: number;
  token: string;
  created: string;
}

export interface AccessStatus {
  grants: Grant[];
  musicians_can_write: boolean;
  guest: Role;
  /** where other devices reach this tabula */
  urls: string[];
  /** false when tabula listens on this machine only */
  reachable: boolean;
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

export async function call<T>(method: "GET" | "POST" | "DELETE", url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
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
}

export const api = {
  status: () => call<Status>("GET", "/api/status"),
  discover: () => call<Discovered[]>("GET", "/api/discover"),
  connect: (addr: string) => call<Status>("POST", "/api/connect", { addr }),
  disconnect: () => call<Status>("POST", "/api/disconnect", {}),
  arm: (armed: boolean) => call<Status>("POST", "/api/arm", { armed }),
  schema: () => call<SchemaResponse>("GET", "/api/schema"),
  state: (prefix?: string) =>
    call<StateNode[]>("GET", "/api/state" + (prefix ? "?prefix=" + encodeURIComponent(prefix) : "")),
  set: (body: SetBody) => call<void>("POST", "/api/set", body),
  whoami: () => call<Whoami>("GET", "/api/whoami"),
  logout: () => call<Whoami>("POST", "/api/logout", {}),
};

export const accessApi = {
  list: () => call<AccessStatus>("GET", "/api/access"),
  add: (name: string, role: Role, bus?: number) => call<Grant>("POST", "/api/access", { name, role, bus }),
  remove: (id: string) => call<void>("DELETE", "/api/access/" + encodeURIComponent(id)),
  settings: (musicians_can_write: boolean) => call<AccessStatus>("POST", "/api/access/settings", { musicians_can_write }),
};
