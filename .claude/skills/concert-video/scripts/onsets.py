# Where a song's music starts, from the multitrack: walking back from a time
# known to be inside the song (its first sung line), the instruments play
# with gaps shorter than GAP seconds; the first note after the last longer
# silence is the start (a reference note given for the singers counts). A song
# starting a cappella starts on the voices. Chapters then start 1 s before.
#   onsets.py SET "t1 t2 …"   → for each time, the music start in session seconds
# project.json "onsets": {"instr": {track: dBFS RMS above which it plays}, "gap": 2.5}:
# DI tracks separate playing from talk well; mics near the talk (hand
# percussion, drum mics) need a high threshold or none. "voices": the singers' tracks.
import sys, os
import numpy as np
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project

CFG = project(); P = CFG["dir"]
O = CFG.get("onsets", {})
INSTR = {int(k): v for k, v in O.get("instr", {}).items()}
GAP = O.get("gap", 2.5)
VOICES = CFG.get("voices", [])

def activity(set_name, chans, above=12.0):
    L = np.load(f"{P}/analysis/{set_name}-levels.npz"); off = float(L["offset"])
    act = None
    for c in chans:
        r = L[f"rms_{c}"]
        a = r > (chans[c] if isinstance(chans, dict) else np.percentile(r, 10) + above)
        act = a if act is None else (act | a)
    return act, off

def music_start(set_name, t_in, limit=90.0):
    act, off = activity(set_name, INSTR)
    voc, _ = activity(set_name, VOICES, 18.0)
    i = int((t_in - off) / 0.25); i0 = max(0, i - int(limit / 0.25)); g = int(GAP / 0.25)
    start = None; quiet = 0
    for j in range(i, i0, -1):
        if act[j]: start, quiet = j, 0
        else:
            quiet += 1
            if quiet >= g and start is not None: break
    if start is None or (i - start) * 0.25 > limit:  # a cappella: the voices
        j = i
        while j > i0 and voc[j - 1]: j -= 1
        start = j
    return off + start * 0.25

if __name__ == "__main__":
    for t in sys.argv[2].split():
        s = music_start(sys.argv[1], float(t)); print(f"{float(t):8.1f} → {s:8.2f}")
