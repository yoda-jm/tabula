// Scene files kept on the machine running tabula (-scenes): listed, sent
// to the console, saved from it, kept from the browser (docs/API.md,
// "Scene files").

import { ApiError, call } from "./api";

export interface SceneFile {
  /** "GVO-2026.scn" */
  file: string;
  /** the scene's name and note, from the file's first line */
  name: string;
  note?: string;
  size: number;
  modified: string;
}

/** SceneProgress is a file being sent to the console ("scenefile" events). */
export interface SceneProgress {
  file: string;
  sent: number;
  total: number;
  done: boolean;
  /** lines this console does not know (another model or firmware) */
  skipped?: string[];
  error?: string;
}

export interface SceneFiles {
  dir: string;
  files: SceneFile[];
  loading?: SceneProgress;
}

const fileUrl = (file: string) => `/api/scenefiles/${encodeURIComponent(file)}`;

export const sceneFilesApi = {
  list: () => call<SceneFiles>("GET", "/api/scenefiles"),
  /** save writes the console's settings as a file; it answers the file name */
  save: (name: string, note: string, replace = false) =>
    call<{ file: string }>("POST", "/api/scenefiles", { name, note, replace }),
  /** load answers once every line is sent: no time limit (the progress comes as "scenefile" events) */
  load: (file: string) => call<SceneProgress>("POST", fileUrl(file) + "/load", {}, 0),
  remove: (file: string) => call<void>("DELETE", fileUrl(file)),
  downloadUrl: fileUrl,
  /** keep sends a file opened in the browser; a 409 ApiError when the name is taken */
  async keep(file: string, text: string, replace = false): Promise<void> {
    const res = await fetch(fileUrl(file) + (replace ? "?replace=1" : ""), {
      method: "PUT",
      headers: { "Content-Type": "text/plain; charset=utf-8" },
      body: text,
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
  },
};

/** sceneFileName is the file a name is saved as, as the server makes it: "Set 1" → "Set 1.scn". */
export function sceneFileName(name: string): string {
  let f = [...name.trim()].map((c) => (c < " " || c === "\x7f" || '/\\:*?"<>|'.includes(c) ? "-" : c)).join("");
  f = f.replace(/^[. ]+|[. ]+$/g, "");
  return (f || "scene").slice(0, 100) + ".scn";
}

/** headerOf reads the name and note of a scene file's first line: #4.0# "name" "note" …. Nothing is escaped
 * in scene files: a quote ends a string only before a blank, as the server reads them. */
export function headerOf(text: string): { name: string; note: string } | null {
  const str = String.raw`"((?:[^"\n]|"(?!\s|$))*)"(?=\s|$)`;
  const m = new RegExp(String.raw`^#[^#\n]*#\s*${str}(?:\s+${str})?`).exec(text.trimStart());
  return m ? { name: m[1], note: m[2] ?? "" } : null;
}

/** looksLikeScene tells a scene file from another file opened by mistake. */
export const looksLikeScene = (text: string) => /^\/[a-z-]/m.test(text) && headerOf(text) !== null;
