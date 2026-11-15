# A picture of a set: every track's level (0.25 s) as rows, the console moves
# underneath and the FOH level, to find the parts and the songs by eye.
#   timeline.py SET OUT.png [FROM_MIN TO_MIN]    (session minutes)
import sys, os, json, datetime
import numpy as np, matplotlib
matplotlib.use("Agg"); import matplotlib.pyplot as plt
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, meta, session_dir

cfg = project(); name, out = sys.argv[1], sys.argv[2]
t0m = float(sys.argv[3]) if len(sys.argv) > 3 else 0; t1m = float(sys.argv[4]) if len(sys.argv) > 4 else 1e9
m = meta(cfg, name); L = np.load(f"{cfg['dir']}/analysis/{name}-levels.npz")
st = datetime.datetime.fromisoformat(m["start"][:26]); off = float(L["offset"])
tracks = [(t["channel"], t["name"]) for t in m["audio"]["tracks"]]
rows = np.array([L[f"rms_{c}"] for c, _ in tracks]); n = rows.shape[1]
t = np.arange(n) * 0.25 + off
sel = (t >= t0m*60) & (t <= t1m*60)
fig, ax = plt.subplots(2, 1, figsize=(28, 11), gridspec_kw={"height_ratios": [3, 1]}, sharex=True)
ax[0].imshow(np.clip(rows[:, sel], -80, -10), aspect="auto", cmap="magma", extent=[t[sel][0]/60, t[sel][-1]/60, len(tracks)-0.5, -0.5], interpolation="nearest")
ax[0].set_yticks(range(len(tracks))); ax[0].set_yticklabels([f"{c} {nm}" for c, nm in tracks], fontsize=8)
ev = [json.loads(l) for l in open(os.path.join(session_dir(cfg, name), "events.jsonl"))]
dly = f"/fx/{cfg['delay_fx']}/par/01" if cfg.get("delay_fx") else "-"
def mark(cond, color, lab, y):
    xs = [e["t"]/60 for e in ev if "addr" in e and cond(e) and t0m*60 <= e["t"] <= t1m*60]
    ax[1].scatter(xs, [y]*len(xs), s=12, c=color, label=lab)
mark(lambda e: e["addr"] == dly, "tab:blue", "delay time", 5)
mark(lambda e: e["addr"].endswith("/mix/on") or e["addr"].startswith("/config/mute"), "tab:red", "mute", 4)
mark(lambda e: "/eq/" in e["addr"] or "/dyn/" in e["addr"] or "/preamp/" in e["addr"], "tab:green", "eq/dyn/preamp", 3)
mark(lambda e: e["addr"].endswith("fader") and not e["addr"].startswith("/auxin"), "tab:gray", "faders", 2)
mark(lambda e: e["addr"].startswith("/-stat/tape/state") or e["addr"].startswith("/auxin"), "tab:orange", "USB player / aux in (break music)", 1)
fl, fr = cfg.get("foh", {}).get("tracks", [23, 24])
foh = np.maximum(L[f"rms_{fl}"], L[f"rms_{fr}"])
ax2 = ax[1].twinx(); ax2.plot(t[sel]/60, foh[sel], lw=0.5, c="k"); ax2.set_ylabel("FOH dBFS")
ax[1].legend(loc="upper left", fontsize=8); ax[1].set_yticks([])
ticks = np.arange(np.ceil(t[sel][0]/60), t[sel][-1]/60, 1)
ax[1].set_xticks(ticks); ax[1].set_xticklabels([(st + datetime.timedelta(minutes=float(x))).strftime("%H:%M") for x in ticks], rotation=90, fontsize=7)
ax[0].grid(axis="x", alpha=0.3); ax[1].grid(axis="x", alpha=0.3)
plt.tight_layout(); plt.savefig(out, dpi=70)
