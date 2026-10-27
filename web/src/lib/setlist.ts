// The setlist: the show's songs, typed before it, with the next one to
// mark; one tap (top bar, M key) marks it with its name, "3. Song name".
// tabula keeps it (docs/API.md, "Setlist") and every engineer page shows
// the next song.

import { call } from "./api";
import type { Record, SessionEvent } from "./session";

export interface Setlist {
  songs: string[];
  /** the next song to mark, from 0; songs.length once all are marked */
  next: number;
  updated?: string;
}

/** Marked is the answer to a marker: the record status and the marker added. */
export interface Marked extends Record {
  marked: { label: string; t: number };
}

export const setlistApi = {
  get: () => call<Setlist>("GET", "/api/setlist"),
  /** save replaces the songs; without next, the next song stays the same one */
  save: (songs: string[], next?: number) => call<Setlist>("PUT", "/api/setlist", next === undefined ? { songs } : { songs, next }),
  /** moveTo makes song n (from 0) the next one: back one song, skip one */
  moveTo: (next: number) => call<Setlist>("POST", "/api/setlist/next", { next }),
  /** markNext marks the next song (a timed marker when none is left) */
  markNext: () => call<Marked>("POST", "/api/record/mark", { next: true }),
  markSong: (song: number) => call<Marked>("POST", "/api/record/mark", { song }),
  /** markLabel marks with a label of yours, a timed marker when empty */
  markLabel: (label: string) => call<Marked>("POST", "/api/record/mark", { label }),
};

/**
 * parseSetlist reads songs typed one per line: blank lines are dropped,
 * spaces tidied, and a number typed in front ("3. ", "03 - ", "3)") left
 * out, since tabula numbers the songs itself ("2:00 AM" stays whole).
 */
export function parseSetlist(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.replace(/^\s*\d{1,3}\s*[.):\-–](?!\d)\s*/, "").replace(/\s+/g, " ").trim())
    .filter(Boolean);
}

/** songLabel is the marker of song i (from 0): "3. Song name". */
export const songLabel = (i: number, song: string) => `${i + 1}. ${song}`;

/** nextSong is the label of the next song to mark, or null when there is none left. */
export const nextSong = (s: Setlist | null) => (s && s.next < s.songs.length ? songLabel(s.next, s.songs[s.next]) : null);

/**
 * songTimes finds when each song was marked in a session (the first marker
 * labelled with it, its number or not), undefined for a song not marked.
 */
export function songTimes(songs: string[], markers: SessionEvent[] = []): (number | undefined)[] {
  return songs.map((song, i) => {
    const m =
      markers.find((e) => e.marker === songLabel(i, song)) ?? markers.find((e) => e.marker?.replace(/^\d+\. /, "") === song);
    return m?.t;
  });
}
