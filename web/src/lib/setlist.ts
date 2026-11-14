// The setlist: the show's songs, typed before it, with the next one to
// mark; one tap (top bar, M key) marks it with its name, "3. Song name".
// A song can have a tempo, typed after its name ("Song name 120 bpm"): the
// FX page sets the delays to the tempo of the song playing.
// tabula keeps it (docs/API.md, "Setlist") and every engineer page shows
// the next song.

import { call } from "./api";
import type { Record, SessionEvent } from "./session";

export interface Setlist {
  songs: string[];
  /** the songs' tempos in BPM, one per song, 0 for none; left out when no song has one */
  tempos?: number[];
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
  save: (songs: string[], tempos: number[], next?: number) => {
    const body: { songs: string[]; tempos?: number[]; next?: number } = { songs };
    if (tempos.some(Boolean)) body.tempos = tempos;
    if (next !== undefined) body.next = next;
    return call<Setlist>("PUT", "/api/setlist", body);
  },
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
  return parseSongs(text).songs;
}

// A tempo after the name: "120 bpm", "· 92.5 BPM", "(120 bpm)", "- 120bpm".
const TEMPO = /\s*[·(,\-–]?\s*(\d{2,3}(?:[.,]\d)?)\s*bpm\s*\)?\s*$/i;

/**
 * parseSongs reads songs typed one per line as parseSetlist does, with the
 * tempo typed after a name ("Song name 120 bpm"), 0 for a song without one.
 */
export function parseSongs(text: string): { songs: string[]; tempos: number[] } {
  const songs: string[] = [];
  const tempos: number[] = [];
  for (const line of text.split(/\r?\n/)) {
    let song = line.replace(/^\s*\d{1,3}\s*[.):\-–](?!\d)\s*/, "").replace(/\s+/g, " ").trim();
    let tempo = 0;
    const m = TEMPO.exec(song);
    if (m && m.index > 0) {
      tempo = Number(m[1].replace(",", "."));
      song = song.slice(0, m.index).trim();
    }
    if (!song) continue;
    songs.push(song);
    tempos.push(tempo);
  }
  return { songs, tempos };
}

/** songTempo is the tempo of song i (from 0), 0 when it has none. */
export const songTempo = (s: Setlist | null, i: number) => s?.tempos?.[i] ?? 0;

/** songLine is a song as the setlist shows it to be edited: "Song name · 120 BPM". */
export const songLine = (song: string, tempo = 0) => (tempo ? `${song} · ${Math.round(tempo * 10) / 10} BPM` : song);

/** setlistText is the setlist as it is edited, one song per line. */
export const setlistText = (s: Setlist) => s.songs.map((song, i) => songLine(song, songTempo(s, i))).join("\n");

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
