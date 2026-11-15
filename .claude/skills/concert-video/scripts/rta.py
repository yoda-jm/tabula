# Every track's spectrum for each video frame, as the console's RTA draws
# behind an EQ: 60 bands from 22 Hz to 20 kHz (1/6 octave), the power of the
# last 8192 samples (170 ms, Hann) up to the end of the frame, in dB.
#   rta.py SET    → analysis/<set>-rta.npz: rta_N (frames × bands, float16), bands (centres, Hz)
# A recorded input is the channel before its EQ: the drawing shows what the EQ
# acts on, and adds the EQ curve to it for what comes out.
import sys, os
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, meta, tracks, blocks

cfg = project(); name = sys.argv[1]; rate = meta(cfg, name)["audio"]["rate"]
HOP, N = rate // 30, 8192
edges = np.geomspace(22, 20000, 61); centres = np.sqrt(edges[:-1] * edges[1:])
freqs = np.fft.rfftfreq(N, 1 / rate)
# each band: the bins inside it, or the nearest one when it is narrower than a bin
groups = []
for lo, hi, c in zip(edges[:-1], edges[1:], centres):
    idx = np.nonzero((freqs >= lo) & (freqs < hi))[0]
    groups.append(idx if len(idx) else np.array([int(np.argmin(np.abs(freqs - c)))]))
M = np.zeros((len(freqs), len(groups)), np.float32)
for j, idx in enumerate(groups): M[idx, j] = 1.0 / len(idx)
win = np.hanning(N).astype(np.float32); norm = 2.0 / (win.sum() ** 2)
out = {}
for ch, nm, p in tracks(cfg, name):
    rows = []; tail = np.zeros(N - HOP, np.float32)
    for v in blocks(p, HOP * 1800, rate):  # a minute of frames at a time
        m = len(v) // HOP
        if not m: continue
        x = np.concatenate([tail, v[:m * HOP]])
        frames = np.lib.stride_tricks.sliding_window_view(x, N)[::HOP][:m]  # window ending at each frame's end
        spec = np.abs(np.fft.rfft(frames * win, axis=1)) ** 2 * norm
        rows.append((10 * np.log10(spec @ M + 1e-14)).astype(np.float16))
        tail = x[-(N - HOP):]
    out[f"rta_{ch}"] = np.concatenate(rows)
    print(ch, nm, len(out[f"rta_{ch}"]), "frames", flush=True)
A = os.path.join(cfg["dir"], "analysis"); os.makedirs(A, exist_ok=True)
np.savez_compressed(f"{A}/{name}-rta.npz", fps=30, offset=meta(cfg, name)["audio"]["offset"], bands=centres.astype(np.float32), **out)
