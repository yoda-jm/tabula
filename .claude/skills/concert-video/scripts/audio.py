# The video's sound: the FOH stereo tracks (project.json "foh") of each part
# of analysis/segments.json, with a short fade at each cut, joined, then one
# gain to the target loudness and a peak limiter at 4× the rate (true peaks
# caught too); no other processing. Writes audio/concert-master.wav and
# prints the loudness before and after. Run from the project folder.
import sys, os, re, subprocess
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, segments, meta, track_file

cfg = project(); P = cfg["dir"]; seg = segments(cfg)
lo = cfg.get("loudness", {}); TARGET, CEIL = lo.get("target", -14.0), lo.get("ceiling", 0.85)
fl, fr = cfg.get("foh", {}).get("tracks", [23, 24])
os.makedirs(f"{P}/audio", exist_ok=True)
parts = []
for i, s in enumerate(seg["sets"]):
    off = meta(cfg, s["name"])["audio"]["offset"]
    a, b = s["a"] - off, s["b"] - off; d = b - a
    fin, fout = s.get("fade_in", 0.5), s.get("fade_out", 1.5)
    out = f"{P}/audio/part{i+1}.wav"
    cmd = ["ffmpeg", "-nostdin", "-loglevel", "error", "-y"]
    for c in (fl, fr): cmd += ["-ss", f"{a:.4f}", "-to", f"{b:.4f}", "-i", track_file(cfg, s["name"], c)]
    cmd += ["-filter_complex", f"[0:a][1:a]join=inputs=2:channel_layout=stereo,afade=t=in:d={fin},afade=t=out:st={d - fout:.4f}:d={fout}[a]",
            "-map", "[a]", "-c:a", "pcm_s24le", out]
    subprocess.run(cmd, check=True); parts.append(out)
lst = f"{P}/audio/parts.txt"
open(lst, "w").write("".join(f"file '{p}'\n" for p in parts))
joined = f"{P}/audio/concert-raw.wav"
subprocess.run(["ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", lst, "-c", "copy", joined], check=True)

def loudness(path):
    r = subprocess.run(["ffmpeg", "-nostdin", "-hide_banner", "-i", path, "-af", "ebur128=peak=true:framelog=quiet", "-f", "null", "-"],
                       capture_output=True, text=True).stderr
    s = r[r.rfind("Summary"):]
    return float(re.search(r"I:\s+(-?[\d.]+) LUFS", s).group(1)), float(re.search(r"Peak:\s+(-?[\d.]+) dBFS", s).group(1))
i0, p0 = loudness(joined)
gain = TARGET - i0
master = f"{P}/audio/concert-master.wav"
# no soxr in every ffmpeg: swr with a long filter is as clean here
subprocess.run(["ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", joined, "-af",
                f"volume={gain:.2f}dB,aresample=192000:filter_size=64:cutoff=0.97,alimiter=limit={CEIL}:attack=5:release=80:level=false:latency=true,"
                "aresample=48000:filter_size=64:cutoff=0.97", "-c:a", "pcm_s24le", master], check=True)
i1, p1 = loudness(master)
print(f"joined: {i0:.1f} LUFS, true peak {p0:.1f} dBFS; gain {gain:+.1f} dB → {i1:.1f} LUFS, true peak {p1:.1f} dBFS")
