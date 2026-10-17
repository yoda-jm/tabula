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
  origin: "console" | "local";
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

export async function call<T>(method: "GET" | "POST", url: string, body?: unknown): Promise<T> {
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
};
