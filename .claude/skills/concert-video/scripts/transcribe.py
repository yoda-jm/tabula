# What is sung and said in a set, to recognise the songs and the talk:
# Whisper (medium, fp32: fp16 gives NaN on some GPUs) over the singers' mics
# mixed to mono 16 kHz (cleaner than the FOH mix), in 30 s pieces with the
# language detected per piece among project.json "languages".
# Writes analysis/<set>-whisper.jsonl: one line per piece, session times.
#   transcribe.py SET [FROM TO]     (session seconds; default the whole set)
# Needs a Python with openai-whisper and soundfile.
import sys, os, json, subprocess
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, meta, track_file
import numpy as np, soundfile as sf, whisper, torch

cfg = project(); name = sys.argv[1]; P = cfg["dir"]
m = meta(cfg, name); off = m["audio"]["offset"]
a = float(sys.argv[2]) if len(sys.argv) > 2 else off
b = float(sys.argv[3]) if len(sys.argv) > 3 else off + m["audio"]["frames"] / m["audio"]["rate"]
wav = f"{P}/audio/{name}-voices16k.wav"
if not os.path.exists(wav):
    os.makedirs(f"{P}/audio", exist_ok=True)
    files = [track_file(cfg, name, c) for c in cfg["voices"]]
    cmd = ["ffmpeg", "-nostdin", "-loglevel", "error", "-y"]
    for f in files: cmd += ["-i", f]
    cmd += ["-filter_complex", f"amix=inputs={len(files)}:normalize=0,volume=8,aresample=16000", "-ac", "1", "-c:a", "pcm_s16le", wav]
    subprocess.run(cmd, check=True)
audio, sr = sf.read(wav, dtype="float32"); assert sr == 16000
langs = cfg.get("languages", ["fr", "en"])
model = whisper.load_model("medium", device="cuda" if torch.cuda.is_available() else "cpu")
with open(f"{P}/analysis/{name}-whisper.jsonl", "w") as f:
    t = a
    while t < b:
        i = int((t - off) * sr); clip = audio[i:i + 30 * sr]
        if len(clip) < sr: break
        mel = whisper.log_mel_spectrogram(whisper.pad_or_trim(clip), n_mels=model.dims.n_mels).to(model.device)
        _, probs = model.detect_language(mel)
        lang = max(langs, key=lambda l: probs.get(l, 0))
        r = model.transcribe(clip, language=lang, fp16=False, condition_on_previous_text=False, no_speech_threshold=0.6)
        segs = [{"s": round(t + s["start"], 2), "e": round(t + s["end"], 2), "text": s["text"].strip(), "nsp": round(s["no_speech_prob"], 2)}
                for s in r["segments"]]
        f.write(json.dumps({"t": t, "lang": lang, "p": round(probs.get(lang, 0), 2), "segs": segs}, ensure_ascii=False) + "\n"); f.flush()
        t += 30
