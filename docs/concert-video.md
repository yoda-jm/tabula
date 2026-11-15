---
title: Concert video
---

# A video of the concert, with the console as it moved

tabula records a show as the console's moves and the audio tracks of its
card. The **concert-video** skill, in `.claude/skills/concert-video` of the
repository, turns those sessions into one video for YouTube: the
front-of-house stereo as the sound, chapters for the songs, and the console
drawn as it moved, every change told in a chat.

![The demo concert: the strips with their EQ over the RTA of their tracks, the lead voice's EQ being changed in the detail panel (the input's spectrum, the EQ curve with the previous one ghosted, the spectrum after the EQ), the chat of the changes, and the progress with the chapters](img/concert-video.png)

- **The console**: inputs, effects, DCAs and outputs, each with its
  scribble strip, a mini EQ curve over the **RTA** of its track, the
  compressor, MUTE, meter and fader. The strip being changed lights amber;
  a muted strip is greyed. Strips silent all along and not recorded are
  left out.
- **The detail**: what is being changed, drawn: an EQ over the input's
  spectrum and the spectrum after it, with the previous curve ghosted; a
  compressor's curve; an effect's parameters, a delay's tempo in BPM.
- **The chat**, *At the desk*: each change in words, what the engineer
  meant first and then the change itself: *Cut the low end on Theo's voice
  (Vx Theo EQ 2 −4.0 → −8.0 dB, 296 Hz → 249 Hz)*.
- **The sound**: the front-of-house stereo the audience heard (often a
  matrix pair, not Main LR), the sets joined, false starts and the break
  cut, one gain to −14 LUFS and a peak limiter.
- **Chapters**: one per song from where its music starts, the spoken
  parts (greetings, the band's introduction, thanks) and the intermission
  (from the end of the set to the next one, the break itself being cut),
  in English or French, each at least 10 s long as YouTube wants them,
  ready for the video's description and embedded in the MP4.

The video is 2560×1072 at 30 fps (H.264, AAC). It is made by
[Claude Code](https://claude.com/claude-code) following the skill's
`SKILL.md`, with Python scripts doing the heavy work; the scripts also run
by hand.

## What it needs

- The sessions of the show (one per set), with their audio tracks; tracks
  compressed to FLAC are read as they are.
- ffmpeg; Python 3 with numpy, pycairo and PyGObject (Pango) for the
  drawing; tabula built (`make build`).
- Optional: a Python with [openai-whisper](https://github.com/openai/whisper)
  (and torch, soundfile, scipy, matplotlib) to recognise the songs and the
  talk from the singers' mics, when no markers give the songs' times. A GPU
  makes it quicker.
- A setlist, from whatever there is: the **markers** set during the show
  (with the setlist, one tap marks the next song), the setlist typed in
  tabula, a text file, or a setlist server.

Where things are on your computer goes in `.env` at the root of the
repository, kept out of git; `.env.example` lists the keys (the sessions
folder, the folder of the video projects, the Whisper Python…). Each video
has its own folder with a `project.json`: the titles, the language, which
card tracks are the front-of-house mix, the effects, how the chat names
each strip.

## How it goes

1. The console at the start of each set, read from the session by a
   throwaway emulator and tabula.
2. Every track's level and spectrum, frame by frame.
3. The songs: the setlist, placed by the markers or by Whisper on the
   singers' mics; each song's start found on the instrument tracks; the
   parts kept and the chapters written down, then checked by you.
4. The console's changes grouped into gestures and put into words.
5. The sound joined and brought to −14 LUFS.
6. The frames drawn (Cairo) and encoded in pieces, then joined with the
   sound and the chapters. A concert of 1 h 40 takes about 1 h 40 on four
   cores.

`scripts/demo/make_demo.sh` does all of it on a made-up show: tabula
records the demo scene from the emulator, with generated audio, a few
moves at the desk and two songs marked from the setlist. The CI runs it;
the picture above comes from it.

## Only the chapters

For a video of a recording as it is, a session's **YouTube chapters**
export gives its markers as chapters, with nothing else to install
([recording](recording.md#youtube-chapters)).

## Why a strip looks wrong

The skill also answers questions about a strip, from the recordings: a
meter moving with another instrument (a mic hearing the cajon next to it,
its gain too high), a strip muted while it was played, a track clipping.
It checks the routing, compares the tracks (correlation, delay, the notes a
tuned instrument plays), and reads the desk's history.
