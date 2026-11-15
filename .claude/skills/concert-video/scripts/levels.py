# Every track's level of a set, in one pass over its files:
#   analysis/<set>-levels.npz   RMS and peak (dBFS) every 0.25 s (rms_N, peak_N)
#   analysis/<set>-meters30.npz peak (dBFS) every video frame, 1/30 s (pk_N)
# N is the card track. Times count from the audio's first sample, which is
# `offset` (stored in both) seconds into the session.
#   levels.py SET
import sys, os
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, meta, tracks, blocks

cfg = project(); name = sys.argv[1]; rate = meta(cfg, name)["audio"]["rate"]
H4, H30 = rate // 4, rate // 30
lv, mt = {}, {}
for ch, nm, p in tracks(cfg, name):
    rms, pk, pk30 = [], [], []
    for v in blocks(p, H4 * 240, rate):  # a minute at a time: a whole number of both hops
        m = len(v) // H4
        if m:
            x = v[:m * H4].reshape(m, H4)
            rms.append(10 * np.log10(np.maximum((x * x).mean(1), 1e-12))); pk.append(20 * np.log10(np.maximum(np.abs(x).max(1), 1e-9)))
        m = len(v) // H30
        if m: pk30.append(20 * np.log10(np.maximum(np.abs(v[:m * H30].reshape(m, H30)).max(1), 1e-6)))
    lv[f"rms_{ch}"] = np.concatenate(rms).astype(np.float32); lv[f"peak_{ch}"] = np.concatenate(pk).astype(np.float32)
    mt[f"pk_{ch}"] = np.concatenate(pk30).astype(np.float16)
    print(ch, nm, len(lv[f"rms_{ch}"]) / 240, "min", flush=True)
off = meta(cfg, name)["audio"]["offset"]
A = os.path.join(cfg["dir"], "analysis"); os.makedirs(A, exist_ok=True)
np.savez_compressed(f"{A}/{name}-levels.npz", hop_s=0.25, offset=off, **lv)
np.savez_compressed(f"{A}/{name}-meters30.npz", fps=30, offset=off, **mt)
