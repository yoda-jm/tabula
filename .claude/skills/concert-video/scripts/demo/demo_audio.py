# A made-up show for the demo session, as a console's card would deliver
# it: raw s32le samples, interleaved, in real time, on the demo scene's
# routing (1-16 the stage, 17-20 the FX sends, 21-22 delay speakers, 23-24
# the FOH mix). Talk, a song in D minor at 100 BPM, talk, a song in A minor
# at 120 BPM, then the room's hiss until stopped.
#   demo_audio.py CHANNELS RATE      (tabula -audio-cmd "python3 demo_audio.py {channels} {rate}")
import sys, time
import numpy as np

CH, RATE = int(sys.argv[1]), int(sys.argv[2])
BLOCK = RATE // 10
rng = np.random.default_rng(7)
NOTE = lambda n: 440.0 * 2 ** ((n - 69) / 12)
SONGS = [  # start, end, BPM, chords (MIDI roots, minor/major), melody (MIDI) per beat
    (10.0, 42.0, 100, [(50, "m"), (46, ""), (53, ""), (48, "")], [62, 65, 69, 67, 65, 64, 62, 60]),
    (52.0, 84.0, 120, [(57, "m"), (53, ""), (48, ""), (55, "")], [69, 72, 71, 69, 67, 69, 64, 65]),
]
TALK = [(3.0, 9.0, 12), (44.0, 50.0, 13)]
HANG = [50, 57, 58, 60, 62, 64, 65, 69]  # a D minor hang

def chord(root, kind):
    return [root, root + (3 if kind == "m" else 4), root + 7]

def harmonics(f, t, weights):
    return sum(w * np.sin(2 * np.pi * f * k * t) for k, w in enumerate(weights, 1))

def partials(phase, weights):
    """A tone from its phase (radians), for pitches that move."""
    return sum(w * np.sin(k * phase) for k, w in enumerate(weights, 1))

def plucks(t, start, period, decay, offset=0.0):
    """The envelope of a note struck every period seconds from start + offset."""
    ph = (t - start - offset) % period
    return np.where(t >= start + offset, np.exp(-ph / decay), 0.0)

def block(t):
    out = np.zeros((CH, len(t)))
    for a, b, bpm, chords, melody in SONGS:
        on = (t >= a) & (t < b)
        if not on.any(): continue
        beat = 60.0 / bpm; bar = 4 * beat
        k = ((t - a) // bar).astype(int) % len(chords)
        roots = np.array([chords[i][0] for i in k]); minor = np.array([chords[i][1] == "m" for i in k])
        fade = np.clip((t - a) / 0.05, 0, 1) * np.clip((b - t) / 1.5, 0, 1) * on
        for n, iv in enumerate((0, 7, 12, 15)):  # strummed chords: autoharp (6), acoustic (9), electric (8), ukulele (10)
            f = NOTE(roots + 12 + (np.where(minor, 15, 16) if iv == 15 else iv))  # root, fifth, octave, third
            out[5] += 0.10 * plucks(t, a, beat, 0.35, n * 0.012) * harmonics(f, t, [1, .5, .3, .2]) * fade
            out[8] += 0.12 * plucks(t, a, beat / 2, 0.25, n * 0.008) * harmonics(f / 2, t, [1, .6, .3]) * fade
            if bpm > 110:
                out[7] += 0.08 * plucks(t, a, beat, 0.6) * harmonics(f / 2, t, [1, .8, .6, .4, .3, .2]) * fade
                out[9] += 0.10 * plucks(t, a, beat, 0.12, beat / 2) * harmonics(f, t, [1, .4]) * fade
        thump = plucks(t, a, beat * 2, 0.08) * np.sin(2 * np.pi * 70 * t)  # cajon: bass on 1 and 3, slaps on 2 and 4
        slap = plucks(t, a, beat * 2, 0.04, beat) * rng.normal(0, 1, len(t))
        out[1] += (0.5 * thump + 0.25 * slap) * fade
        if bpm > 110: out[2] += 0.15 * plucks(t, a, beat, 0.06, beat / 2) * rng.normal(0, 1, len(t)) * fade  # brushes
        else:  # the hang, one note a beat
            idx = ((t - a) // beat).astype(int) * 3 % len(HANG)
            out[0] += 0.25 * plucks(t, a, beat, 0.9) * harmonics(NOTE(np.array(HANG)[idx]), t, [1, .3, .15]) * fade
        mel = np.array(melody)[((t - a) // beat).astype(int) % len(melody)]
        env = np.sin(np.pi * ((t - a) % beat) / beat) ** 0.6  # each note sung, silent at the change
        def sung(n):  # with a vibrato of ±0.4 % at 5.5 Hz
            f = NOTE(n); return 2 * np.pi * f * t - f * 0.004 / 5.5 * np.cos(2 * np.pi * 5.5 * t)
        lead = 11 if bpm < 110 else 12
        out[lead] += 0.18 * partials(sung(mel), [1, .7, .5, .45, .3, .15, .1]) * env * fade
        out[13] += 0.08 * partials(sung(mel + 4), [1, .6, .4, .3]) * env * fade  # a harmony
    for a, b, ch in TALK:
        on = (t >= a) & (t < b)
        if on.any():
            syll = np.clip(np.sin(2 * np.pi * 3.7 * t) + 0.3 * np.sin(2 * np.pi * 1.3 * t), 0, 1)
            ph = 2 * np.pi * 140 * t - 140 * 0.1 / 0.7 * np.cos(2 * np.pi * 0.7 * t)  # a voice rising and falling ±10 %
            out[ch - 1] += 0.12 * syll * partials(ph, [1, .8, .6, .5, .3]) * on
    out[:16] += rng.normal(0, 3e-4, (16, len(t)))  # the room in the mics
    # the desk's sends and mix
    if CH >= 24:
        stage = out[:16]
        out[16] = 0.3 * (stage[11] + stage[12] + stage[13] + 0.5 * stage[0])  # Room
        out[17] = 0.3 * (stage[11] + stage[12] + stage[13])                   # Hall
        out[18] = 0.4 * stage[11]                                             # Delay
        pan = np.linspace(0.3, 0.7, 16)[:, None]
        out[22] = 0.6 * ((1 - pan) * stage).sum(0); out[23] = 0.6 * (pan * stage).sum(0)
        out[20], out[21] = 0.7 * out[22], 0.7 * out[23]
    return out

if __name__ == "__main__":
    n, t0 = 0, time.monotonic()
    w = sys.stdout.buffer
    while True:
        t = (n + np.arange(BLOCK)) / RATE
        x = np.clip(block(t), -1, 1)
        w.write((x.T * 2147483647).astype("<i4").tobytes()); w.flush()
        n += BLOCK
        ahead = n / RATE - (time.monotonic() - t0)
        if ahead > 0: time.sleep(ahead)
