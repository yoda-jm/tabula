---
name: concert-video
description: Make one YouTube video of a live show recorded with tabula (X32/M32 console moves + multitrack): the front-of-house stereo as the sound, YouTube chapters found from the setlist, the markers and the recordings, and the console drawn as it moved — faders, mutes, meters, EQ with the RTA, compressors, effects — with every change told in a chat, intent first. Also explains what a strip's meter really shows (mic bleed, mutes, routing). Use when asked for a concert video, a show's chapters, a console-moves video, or why a strip looks wrong in one.
---

# Concert video from tabula sessions

The result: one MP4 per concert, 2560×1072 (cinemascope) at 30 fps, H.264
CRF 18 with AAC 384 kbps and the chapters embedded, plus `chapters.txt` for
the YouTube description. On screen:

- **top**: the band, venue · town · date, the current chapter, the time;
- **the console**: inputs, effects, DCAs, outputs (Main, the FOH matrix,
  other matrices), each with its scribble strip, mini EQ curve over the
  **RTA** of its track (inputs), compressor (threshold · ratio), MUTE, meter
  and fader with its dB value. The strip being changed lights amber. **A
  muted strip is greyed** (its MUTE stays red; it keeps its colours while a
  change is being made on it);
- **right**: a detail panel drawing what is being changed (the EQ curve
  with the previous one ghosted and the band moved, over the input's RTA and
  the RTA after the EQ; the compressor curve; the effect's parameters; the
  delay's tempo in BPM with a beat), and the chat **AT THE DESK**;
- **bottom**: the concert's progress with a tick per chapter;
- an opening title (7 s), a part card at each set change (4 s), a 3 s fade
  to black at the end.

The scripts are in `scripts/` next to this file. They run from the project
folder and read its `project.json` (copy `scripts/project.example.json`;
every key is explained there).

## Settings: nothing personal in the repository

Where things are on this computer comes from the environment or from
`.env` at the root of the tabula repository (kept out of git; `.env.example`
lists the keys): `TABULA_SESSIONS` (default `sessions/` there),
`TABULA_BIN` (default `bin/`), `CONCERT_VIDEOS` (the folder of the video
projects), `WHISPER_PYTHON`, `TROUBASTACK_URL`. When one is missing and
needed, ask, and offer to write it to `.env`. Logins are asked for each
time and never written down. `scripts/common.py get KEY` prints a setting.

## Defaults (keep them unless told otherwise)

- **One video for the whole concert**, the sets joined. Cut what is not
  the show and say so: false starts (a song stopped and restarted, e.g. the
  voices still muted), the break, and the break music from the console's
  USB player (commercial recordings). A short fade at each cut.
- **The sound is the front-of-house stereo** the audience heard: often a
  matrix pair recorded on two card tracks, not Main LR. Check the routing.
  One gain to −14 LUFS and a peak limiter at 0.85 run at 192 kHz (true peak
  about −1.3 dBTP in the WAV; AAC adds about 0.8 dB). No other processing.
- **YouTube chapters** in the project's language (`lang`, default `en`;
  `chapters.py labels` prints the words):
  ```
  Chapters

  00:00:00 : Title - Artist
  00:03:01 : Title - Artist
  00:18:52 : BAND INTRODUCTION
  ```
  The first at 00:00:00. A song starts 1 s before its first note, or before
  the reference note or chord given to the singers, or on the count-in; the
  talk introducing a song stays in the chapter before. Spoken parts in
  capitals (GREETINGS, BAND INTRODUCTION, THANKS, SPEECH); encores marked
  "(encore)". In French (`"lang": "fr"`): Chapitrage, DEBUT, PRESENTATION DES
  MUSICIENS, REMERCIEMENTS, DISCOURS, "(rappel)". Keep the artists'
  spellings the band used before (earlier chapter lists, the setlist).
- **The chat is in English, intent first, then the real change in
  parentheses**: *Boost the presence on the electric guitar (Gtr EQ 3 +1.5
  → +3.0 dB at 3.20 kHz)*, *Cut the low mids on Name's voice (…)*, *Bring
  down the effects (DCA FX −0.1 → −7.4 dB)*, *Set the delay to the song's
  tempo, about 96 BPM (…)*. While a control moves, the numbers in its line
  follow it. Several mutes at once are one line.
- **Strips shown**: hide what was silent for the whole of the parts shown
  and not recorded (muted, or fader at −∞), effects whose send track is
  silent or that stayed muted, and calibration DCAs (`dca_hidden`). A strip
  that was live but not recorded is shown with an empty meter: if it looks
  out of place, ask, and pin the list with `inputs`.
- **Meters and RTA** are the recorded inputs, before EQ, fader and mute, as
  on the console. Main and the other matrices have no track: their meter is
  the FOH tracks less the FOH matrix's level plus theirs.
- **Re-rendering when a video exists: write a new file** (new `OUT` and
  `PIECES`): the current one may be being watched.

## Steps

0. **Folder and tools.** `$CONCERT_VIDEOS/YYYYMMDD_short_name/` with
   `project.json`. The sessions (one per set) in `TABULA_SESSIONS`: copy them
   off the stage laptop first and verify checksums; compressed tracks
   (FLAC) are read as well. Needs ffmpeg; `python3` with numpy, pycairo and
   PyGObject/Pango (the drawing); `WHISPER_PYTHON` with openai-whisper,
   torch, soundfile, scipy, matplotlib (transcription, timelines,
   spectrograms); tabula built (`make build`) for the state dump.
1. **Read the sessions**: `meta.json` (audio `offset`: the first sample in
   session seconds; `drift_ppm`; the tracks, named by tabula after the
   console's routing), `start.scn`, `events.jsonl` (`{t, addr, text, value}`
   and `{t, marker}`). From the routing (`/config/routing/CARD`,
   `/outputs/main/NN/src`) find the FOH pair, the FX send tracks, which
   tracks are inputs.
2. **Console state**: `scripts/dump_state.sh SET…` → `analysis/<set>-state0.json`,
   `schema.json` (throwaway emulator + tabula on ports 18976/18977, a temp
   folder, only those two processes stopped).
3. **Levels and spectra**: `python3 -I scripts/levels.py SET` →
   `<set>-levels.npz` (0.25 s) and `<set>-meters30.npz` (per frame);
   `scripts/rta.py SET` → `<set>-rta.npz` (the RTA, per frame). Then
   `timeline.py SET out.png` (every track's level with the console moves
   under it) and `gaps.py SET` to see the sets, the break, the songs.
4. **The setlist**, from whatever there is: `setlist.py markers SET…` (the
   markers set during the show: titles and rough times), `setlist.py
   session SET` (the setlist typed in tabula), `setlist.py text FILE` (a
   list, or an earlier chapter list), `setlist.py troubastack BAND CONCERT`
   (`TROUBASTACK_URL`; the login as `TROUBASTACK_USER` /
   `TROUBASTACK_PASSWORD` for that command only). Else ask. →
   `analysis/setlist.json`.
5. **What was sung and said** (needed without marker times, useful as a
   check otherwise): `$WHISPER_PYTHON scripts/transcribe.py SET [FROM TO]` →
   `<set>-whisper.jsonl`. It listens to the singers' mics (`voices`), not the
   FOH mix: lyrics and talk come out clearly. Match the lyrics to the
   setlist; note the talk (greetings, introductions, thanks, false starts,
   "no sound" chaos), songs played out of order or added.
6. **Song starts**: `onsets.py SET "t1 t2 …"` with a time inside each song
   (a marker, its first sung line) → where its music starts (walking back to
   the last silence of `gap` s on the instrument tracks). Tune
   `onsets.instr` per track: DIs with low thresholds; mics that hear the
   talk (hand percussion, drums) high or out. Check by hand: intros under
   talk, count-ins, a cappella.
7. **`analysis/segments.json`**: the parts and the chapters:
   ```json
   {"sets": [{"name": "<set>", "a": 647.26, "b": 3621.0, "fade_in": 0.3, "fade_out": 2.0}],
    "chapters": [{"t": 0.0, "title": "Title - Artist"}]}
   ```
   `a`/`b` in session seconds; chapter `t` in video seconds (the parts
   joined: a part's video start + session time − its `a`). Then
   `chapters.py > chapters.txt` and show it to the user.
8. **The chat**: `changes.py SET` → `<set>-gestures.json` (changes of a
   control less than 2 s apart are one gesture). Read some: names come
   from `spoken` (default "Vx Name" → "Name's voice", else "the <name>").
9. **Sound**: `audio.py` → `audio/concert-master.wav`; it prints the loudness
   before and after.
10. **Look before rendering**: `render.py still out.png SECONDS` at the
    start, a busy EQ change, the part junction, the end; look at them.
11. **Render**: `scripts/make_video.sh` in the background, at a low priority
    (`nice`) when the computer is in use (about 25 ms per frame and core plus
    x264: 1 h 40 of concert took 1 h 40 on 4 cores). Tell the user the
    expected time. `MUX_ONLY=1` re-joins the pieces after a change of sound
    or chapters only (minutes).
12. **Check the file**: ffprobe (duration, 2560×1072, AAC, chapter count),
    a few frames (a chapter change, the junction, the end), `ebur128` on the
    MP4's sound. Write `README.md` in the project folder: the outputs, what
    was cut and why, which strips are hidden, how to redo a step.

Only chapters, for a video of a whole recording as it is: tabula's own
**YouTube chapters** export (a session's details, or
`/api/sessions/{name}/export?format=chapters`) gives the markers as
chapters, without any of this.

## When a strip looks wrong ("is this the cajon on the hang mic?")

`inspect_track.py` (with `WHISPER_PYTHON` for `pitches` and `spectro`):

- the routing: is the card track the channel's own source? (`IN`/`CARD`
  routing, `/ch/NN/config` source, `/-ha/NN` → headamp, its gain and 48 V);
- `N songs --vs M`: per chapter, the track's level, clipping (peaks within
  0.5 dB of full scale), how much it follows track M, how often it is loud
  while M is quiet;
- `N like SET "t…"`: the tracks it resembles. Mic bleed: a low correlation
  at a steady lag of a few ms (sound travels 0.34 m/ms). The same input
  recorded twice: 1.0 at 0 ms;
- `N pitches SET "t…"`: a tuned instrument gives the same notes from song
  to song; a percussion's bleed gives none;
- `N spectro SET "t…" out.png --with M`: hits (vertical lines) or notes
  (horizontal lines);
- the desk history in `events.jsonl`: its mutes, fader, EQ over the show
  (was it open while the other instrument played? muted while played?);
- if needed, what reached the PA: run both tracks through their channels'
  low cut, EQ and fader (the state has them) and compare.

Answer with numbers and times in the video; say what is certain and what is
inferred.

## Pitfalls

- Never play audio on the user's speakers. Never stop or reuse a tabula or
  emulator the user runs; stop only the processes you started.
- Don't change `analysis/` files while a render runs: each piece reads them
  when it starts.
- `python3 -I` leaves the script's folder off `sys.path`: the scripts add it.
- Whisper: fp32 (fp16 gives NaN on some GPUs); the language per 30 s piece,
  limited to `languages`.
- Pango on every frame is too slow: the renderer caches text and mini EQ
  images (keep it that way when changing it).
- ffmpeg may lack soxr and nvenc: the scripts use swr and x264.
- The sound files are big (a 1 h 40 stereo 24-bit WAV is 1.7 GB, and
  `audio.py` keeps the parts, the join and the master): test runs go on a
  disk, not in a `/tmp` held in memory.
