import { describe, expect, it } from "vitest";
import type { CopyStatus, DiskInfo } from "./takehome";
import { COPY_MARGIN, copyText, diskLabel, homeApi, roomFor } from "./takehome";

const disk: DiskInfo = { path: "/media/yoda/BAND", name: "BAND", free: 41.2e9, size: 64e9, writable: true };

describe("taking a show home", () => {
  it("tells whether a session fits on a disk", () => {
    expect(roomFor(disk, 12.3e9)).toBe("fits");
    expect(roomFor(disk, 41.2e9)).toBe("short"); // the margin the server keeps
    expect(roomFor(disk, 41.2e9 - COPY_MARGIN)).toBe("fits");
    expect(roomFor({ ...disk, free: -1 }, 1e9)).toBe("unknown");
    expect(roomFor(disk, 0)).toBe("unknown");
  });

  it("names disks and copies", () => {
    expect(diskLabel(disk)).toBe("BAND (41.2 GB free of 64.0 GB)");
    expect(diskLabel({ ...disk, writable: false })).toBe("BAND (read-only)");
    expect(diskLabel({ ...disk, free: -1, size: -1 })).toBe("BAND");
    const c: CopyStatus = { session: "s", disk: disk.path, folder: "", flac: false, done: 1.2e9, total: 12.3e9, files: 3, of: 35, state: "copying" };
    expect(copyText(c)).toBe("3 of 35 files · 1.2 GB of 12.3 GB");
  });

  it("builds the zip address", () => {
    expect(homeApi.zipUrl("2026-10-25_203512", false)).toBe("/api/sessions/2026-10-25_203512/zip");
    expect(homeApi.zipUrl("a b", true)).toBe("/api/sessions/a%20b/zip?flac=1");
  });
});
