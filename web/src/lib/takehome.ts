// Taking a show home (docs/recording.md, "Taking a show home"): a session
// downloaded as one zip, or copied to a disk plugged into the machine
// running tabula, its tracks as recorded (WAV) or in FLAC. Neither writes
// on that machine's disk.

import { LONG, call } from "./api";
import { formatBytes } from "./audio";

/** DiskInfo is a disk plugged into the machine running tabula (a USB disk). */
export interface DiskInfo {
  /** where it is mounted: /media/yoda/BAND */
  path: string;
  name: string;
  /** bytes, -1 if unknown */
  free: number;
  size: number;
  writable: boolean;
}

export interface Disks {
  disks: DiskInfo[];
  /** ffmpeg is there to convert the tracks to FLAC */
  flac: boolean;
}

/** CopyStatus is a session being copied to a disk, or the last one copied. */
export interface CopyStatus {
  session: string;
  disk: string;
  /** where the session is once complete: <disk>/tabula/<session> */
  folder: string;
  flac: boolean;
  /** bytes read from the disk of the sessions, of total */
  done: number;
  total: number;
  /** files copied, of of */
  files: number;
  of: number;
  /** being copied */
  file?: string;
  /** a copy that does not end done leaves nothing on the disk */
  state: "copying" | "done" | "failed" | "cancelled";
  error?: string;
  /** started while a show was being recorded */
  recording?: boolean;
}

const sessionUrl = (name: string) => "/api/sessions/" + encodeURIComponent(name);

export const homeApi = {
  /** zipUrl downloads a session as one zip, made as it is sent */
  zipUrl: (name: string, flac: boolean) => sessionUrl(name) + "/zip" + (flac ? "?flac=1" : ""),
  disks: () => call<Disks>("GET", "/api/disks"),
  /** copy answers once the copy started (it reads the tracks' headers first) */
  copy: (name: string, disk: string, flac: boolean) => call<CopyStatus>("POST", sessionUrl(name) + "/copy", { disk, flac }, LONG),
  status: () => call<CopyStatus | null>("GET", "/api/copy"),
  stop: () => call<CopyStatus | null>("POST", "/api/copy/stop", {}),
};

/** COPY_MARGIN is the room a copy leaves on a disk, as the server's. */
export const COPY_MARGIN = 16 << 20;

/** roomFor tells whether a disk has room for need bytes. */
export function roomFor(disk: DiskInfo, need: number): "fits" | "short" | "unknown" {
  if (disk.free < 0 || !(need > 0)) return "unknown";
  return disk.free >= need + COPY_MARGIN ? "fits" : "short";
}

/** diskLabel names a disk with its room: "BAND (41.2 GB free of 64.0 GB)". */
export function diskLabel(d: DiskInfo): string {
  if (!d.writable) return `${d.name} (read-only)`;
  if (d.free < 0) return d.name;
  return `${d.name} (${formatBytes(d.free)} free${d.size > 0 ? ` of ${formatBytes(d.size)}` : ""})`;
}

/** copyText says where a copy is: "3 of 35 files · 1.2 GB of 12.3 GB". */
export function copyText(c: CopyStatus): string {
  return `${c.files} of ${c.of} files · ${formatBytes(c.done)} of ${formatBytes(c.total)}`;
}
