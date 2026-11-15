# Candidate gaps between songs: stretches where the FOH mix (max of L/R, 1 s
# RMS) falls 12 dB or more under its median of the surrounding 60 s, for at
# least 1.5 s, with the mutes and delay-time changes around them (an engineer
# often sets the delay to the next song's tempo between songs).
#   gaps.py SET [FROM TO]    (session seconds)
import sys, os, json, datetime
import numpy as np
from numpy.lib.stride_tricks import sliding_window_view as sw
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, meta, session_dir

cfg = project(); name = sys.argv[1]
a = float(sys.argv[2]) if len(sys.argv) > 2 else 0; b = float(sys.argv[3]) if len(sys.argv) > 3 else 1e9
L = np.load(f"{cfg['dir']}/analysis/{name}-levels.npz"); off = float(L["offset"])
st = datetime.datetime.fromisoformat(meta(cfg, name)["start"][:26])
fl, fr = cfg.get("foh", {}).get("tracks", [23, 24])
p = 10**(np.maximum(L[f"rms_{fl}"], L[f"rms_{fr}"])/10)
s1 = 10*np.log10(np.convolve(p, np.ones(4)/4, "same") + 1e-12)  # 1 s
t = np.arange(len(s1))*0.25 + off
med = np.median(sw(np.pad(s1, 120, mode="edge"), 241), axis=1)[:len(s1)]
low = (s1 < med - 12) & (t >= a) & (t <= b)
ev = [json.loads(l) for l in open(os.path.join(session_dir(cfg, name), "events.jsonl"))]
dly = f"/fx/{cfg['delay_fx']}/par/01" if cfg.get("delay_fx") else None
i = 0; out = []
while i < len(low):
    if low[i]:
        j = i
        while j < len(low) and low[j]: j += 1
        if (j - i)*0.25 >= 1.5: out.append((t[i], t[j-1], s1[i:j].min(), med[i]))
        i = j
    else: i += 1
clk = lambda x: (st + datetime.timedelta(seconds=x)).strftime("%H:%M:%S")
for s, e, mn, md in out:
    near = [x for x in ev if "addr" in x and s - 20 <= x["t"] <= e + 20 and (x["addr"] == dly or x["addr"].endswith("/mix/on"))]
    print(f"{clk(s)}–{clk(e)} ({e-s+0.25:4.1f}s) min {mn:5.1f} vs {md:5.1f}  sess {s:7.1f}  {' '.join(sorted(set(x['addr'].replace('/mix/on','/on') for x in near)))[:90]}")
