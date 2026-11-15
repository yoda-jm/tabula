# The chapters of analysis/segments.json for YouTube (a header, then
# "HH:MM:SS : Title" from 00:00:00), or as ffmpeg metadata (chapters.py
# ffmeta), times in video seconds; `chapters.py labels` prints the words for
# the spoken chapters in the project's language ("lang", default en).
# Run from the project folder.
import sys, os, json
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, segments, labels

cfg = project()
if len(sys.argv) > 1 and sys.argv[1] == "labels":
    print(json.dumps(labels(cfg), ensure_ascii=False, indent=1)); sys.exit()

seg = segments(cfg)
ch = sorted(seg["chapters"], key=lambda c: c["t"])
dur = sum(s["b"] - s["a"] for s in seg["sets"])
hms = lambda t: f"{int(t // 3600):02d}:{int(t % 3600 // 60):02d}:{int(t % 60):02d}"
if len(sys.argv) > 1 and sys.argv[1] == "ffmeta":
    print(";FFMETADATA1\ntitle=" + cfg.get("metadata_title", cfg.get("title", "")))
    for i, c in enumerate(ch):
        end = ch[i + 1]["t"] if i + 1 < len(ch) else dur
        print(f"[CHAPTER]\nTIMEBASE=1/1000\nSTART={int(c['t'] * 1000)}\nEND={int(end * 1000)}\ntitle={c['title']}")
else:
    print(labels(cfg)["header"] + "\n")
    for c in ch: print(f"{hms(c['t'])} : {c['title']}")
