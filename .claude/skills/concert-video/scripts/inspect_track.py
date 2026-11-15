# What a track really holds, when a strip's meter looks wrong (a mic that
# seems to play another instrument, a strip moving while muted…):
#   inspect_track.py N songs [--vs M]            per chapter: N's level, clipping, how much it follows M
#   inspect_track.py N like SET "t1 t2 …"        the tracks N resembles most (waveform correlation, lag)
#   inspect_track.py N pitches SET "t1 t2 …"     the strongest steady pitches (a tuned instrument
#                                                  gives the same notes from song to song; bleed of
#                                                  a percussion gives none)
#   inspect_track.py N spectro SET "t1 t2 …" OUT.png [--with M,K]   spectrograms of N (and M, K)
# Times in session seconds, 4 s (8 s for spectro, 40 s for pitches) from each.
# The recorded track is the channel's input, before the fader and the mute:
# the console's input meter shows the same.
import sys, os, json
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, segments, tracks, track_file, meta, read

cfg = project(); P = cfg["dir"]
n = int(sys.argv[1]); mode = sys.argv[2]
db = lambda x: 20*np.log10(np.sqrt(np.mean(x**2)) + 1e-12)
def opt(name, default=None):
    return sys.argv[sys.argv.index(name) + 1] if name in sys.argv else default

def clip(name, ch, t, d):
    return read(track_file(cfg, name, ch), t - meta(cfg, name)["audio"]["offset"], d)

if mode == "songs":
    seg = segments(cfg); sets = seg["sets"]; vs = int(opt("--vs", 0)) or None
    base = [0.0]
    for s in sets[:-1]: base.append(base[-1] + s["b"] - s["a"])
    ch = sorted(seg["chapters"], key=lambda c: c["t"]); dur = base[-1] + sets[-1]["b"] - sets[-1]["a"]
    M = [np.load(f"{P}/analysis/{s['name']}-meters30.npz") for s in sets]
    for i, c in enumerate(ch):
        t0, t1 = c["t"], (ch[i + 1]["t"] if i + 1 < len(ch) else dur)
        k = max(j for j in range(len(sets)) if base[j] <= t0); s = sets[k]; m = M[k]; off = float(m["offset"])
        end = min(t1, base[k + 1] if k + 1 < len(sets) else dur)
        a = int((s["a"] + t0 - base[k] - off)*30); b = int((s["a"] + end - base[k] - off)*30)
        h = m[f"pk_{n}"][a:b].astype(np.float32)
        line = f"{c['title'][:34]:34s} p50 {np.median(h):6.1f} p99 {np.percentile(h, 99):5.1f} clip {np.mean(h > -0.5)*100:5.2f}%"
        if vs:
            r = m[f"pk_{vs}"][a:b].astype(np.float32); on = r > -30
            cor = np.corrcoef(h[on], r[on])[0, 1] if on.sum() > 100 else float("nan")
            line += f" | {vs} p50 {np.median(r):6.1f} | follows {vs} (r while it plays) {cor:+.2f} | loud while {vs} is quiet {np.mean((r < -40) & (h > -20))*100:4.1f}%"
        print(line)
elif mode == "like":
    name = sys.argv[3]
    for t in map(float, sys.argv[4].split()):
        h = clip(name, n, t, 4); out = []
        for c, nm, _ in tracks(cfg, name):
            if c == n: continue
            x = clip(name, c, t, 4); L = 2880; k_ = len(h)
            xc = np.fft.irfft(np.fft.rfft(h, 2*k_) * np.conj(np.fft.rfft(x, 2*k_)))
            xc = np.concatenate([xc[-L:], xc[:L+1]]) / (np.linalg.norm(h)*np.linalg.norm(x) + 1e-12)
            j = int(np.argmax(np.abs(xc))); out.append((abs(xc[j]), f"{c} {nm}", xc[j], (j - L)/48.0, db(x)))
        out.sort(reverse=True)
        print(f"t={t:.0f} track {n} {db(h):.1f} dBFS: " + "; ".join(f"{nm} {r:+.2f} at {l:+.1f} ms ({lv:.0f} dBFS)" for _, nm, r, l, lv in out[:4]))
    print("(+ lag: track", n, "comes later. Bleed: a steady lag of a few ms and a low correlation; a duplicated input: 1.0 at 0 ms)")
elif mode == "pitches":
    from scipy.signal import welch, find_peaks
    name = sys.argv[3]
    note = lambda f: ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"][int(round(12*np.log2(f/440) + 69)) % 12] + str(int(round(12*np.log2(f/440) + 69))//12 - 1)
    for t in map(float, sys.argv[4].split()):
        x = clip(name, n, t, 40); f, p = welch(x, 48000, nperseg=16384); d = 10*np.log10(p + 1e-20)
        band = (f > 90) & (f < 1200); k, pr = find_peaks(d[band], prominence=8)
        top = sorted(zip(pr["prominences"], f[band][k]), reverse=True)[:8]
        print(f"t={t:.0f}: " + "  ".join(f"{fr:6.1f} {note(fr)}" for _, fr in sorted(top, key=lambda z: z[1])))
elif mode == "spectro":
    import matplotlib; matplotlib.use("Agg"); import matplotlib.pyplot as plt
    name, times, out = sys.argv[3], [float(x) for x in sys.argv[4].split()], sys.argv[5]
    chans = [n] + [int(c) for c in opt("--with", "").split(",") if c]
    names = {c: nm for c, nm, _ in tracks(cfg, name)}
    fig, ax = plt.subplots(len(chans), len(times), figsize=(5.5*len(times), 3.3*len(chans)), sharey=True, squeeze=False)
    for j, t in enumerate(times):
        for i, c in enumerate(chans):
            x = clip(name, c, t, 8); a = ax[i, j]
            a.specgram(x, NFFT=2048, Fs=48000, noverlap=1536, cmap="magma", vmin=-130, vmax=-30, scale="dB")
            a.set_ylim(40, 4000); a.set_yscale("log"); a.set_title(f"{t:.0f} s — {c} {names.get(c, '')}  {db(x):.0f} dBFS", fontsize=10)
    plt.tight_layout(); plt.savefig(out, dpi=70)
