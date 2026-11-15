# Shared by the scripts: the settings, the project and the session's tracks.
#
# Where things are on this computer comes from the environment, or from
# the .env file at the root of the tabula repository (not in git; see
# .env.example): TABULA_SESSIONS, TABULA_BIN, CONCERT_VIDEOS,
# WHISPER_PYTHON, TROUBASTACK_URL. The project folder (the current
# directory, or $CONCERT_VIDEO) holds project.json (see
# project.example.json), analysis/, audio/ and render/.
#   common.py get KEY      prints a setting (for the shell scripts)
import json, os, struct, subprocess, sys
import numpy as np

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))))
DEFAULTS = {"TABULA_SESSIONS": os.path.join(REPO, "sessions"), "TABULA_BIN": os.path.join(REPO, "bin"), "WHISPER_PYTHON": "python3"}

def _dotenv():
    out = {}
    try:
        for line in open(os.path.join(REPO, ".env")):
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line: continue
            k, v = line.split("=", 1); out[k.strip()] = v.strip().strip('"').strip("'")
    except OSError:
        pass
    return out

def setting(key):
    """A setting: the environment, then .env, then the default (None when there is none)."""
    v = os.environ.get(key) or _dotenv().get(key) or DEFAULTS.get(key)
    return os.path.expanduser(os.path.expandvars(v)) if v else None

# The words of the chapters and titles, per project.json "lang" (default en).
LABELS = {
    "en": {"header": "Chapters", "greetings": "GREETINGS", "band": "BAND INTRODUCTION", "thanks": "THANKS",
           "speech": "SPEECH", "encore": "(encore)", "part": "Part {n}"},
    "fr": {"header": "Chapitrage", "greetings": "DEBUT", "band": "PRESENTATION DES MUSICIENS", "thanks": "REMERCIEMENTS",
           "speech": "DISCOURS", "encore": "(rappel)", "part": "Partie {n}"},
}

def labels(cfg):
    return {**LABELS["en"], **LABELS.get(cfg.get("lang", "en"), {}), **cfg.get("labels", {})}

def project(p=None):
    p = os.path.abspath(p or os.environ.get("CONCERT_VIDEO") or os.getcwd())
    cfg = json.load(open(os.path.join(p, "project.json")))
    cfg["dir"] = p
    cfg["sessions"] = os.path.expanduser(cfg.get("sessions") or setting("TABULA_SESSIONS"))
    cfg.setdefault("lang", "en")
    return cfg

def session_dir(cfg, name):
    return os.path.join(cfg["sessions"], name)

def meta(cfg, name):
    return json.load(open(os.path.join(session_dir(cfg, name), "meta.json")))

def segments(cfg):
    return json.load(open(os.path.join(cfg["dir"], "analysis", "segments.json")))

def track_file(cfg, name, channel):
    """The file of a recorded card track: the WAV, or the FLAC tabula compressed it to."""
    for t in meta(cfg, name)["audio"]["tracks"]:
        if t["channel"] == channel:
            p = os.path.join(session_dir(cfg, name), t["file"])
            if not os.path.exists(p) and os.path.exists(os.path.splitext(p)[0] + ".flac"):
                p = os.path.splitext(p)[0] + ".flac"
            return p
    return None

def tracks(cfg, name):
    return [(t["channel"], t["name"], track_file(cfg, name, t["channel"])) for t in meta(cfg, name)["audio"]["tracks"]]

def _wav(p):
    """(data offset, data bytes, bits) of a PCM WAV."""
    f = open(p, "rb"); f.read(12); bits = 24
    while True:
        c = f.read(8)
        if len(c) < 8: raise ValueError(f"{p}: no data chunk")
        cid, sz = c[:4], struct.unpack("<I", c[4:])[0]
        if cid == b"fmt ":
            fmt = f.read(sz + (sz & 1)); bits = struct.unpack("<H", fmt[14:16])[0]; continue
        if cid == b"data": return f.tell(), os.path.getsize(p) - f.tell(), bits
        f.seek(sz + (sz & 1), 1)

def _decode(raw, bits):
    if bits == 24:
        b = raw.reshape(-1, 3).astype(np.int32)
        v = b[:, 0] | (b[:, 1] << 8) | (b[:, 2] << 16)
        return np.where(v >= 1 << 23, v - (1 << 24), v).astype(np.float32) / 8388608
    if bits == 16: return raw.view("<i2").astype(np.float32) / 32768
    return raw.view("<i4").astype(np.float32) / 2147483648

def blocks(p, frames, rate=48000):
    """The track as float32 blocks of `frames` samples (the last one shorter)."""
    if p.endswith(".wav"):
        off, n, bits = _wav(p); w = bits // 8; n -= n % w
        mm = np.memmap(p, dtype=np.uint8, mode="r", offset=off, shape=(n,))
        for i in range(0, n, frames * w):
            yield _decode(np.asarray(mm[i:i + frames * w]), bits)
        return
    proc = subprocess.Popen(["ffmpeg", "-nostdin", "-loglevel", "error", "-i", p, "-f", "f32le", "-ac", "1", "-ar", str(rate), "-"],
                            stdout=subprocess.PIPE)
    while True:
        b = proc.stdout.read(frames * 4)
        if not b: break
        yield np.frombuffer(b, "<f4").copy()
    proc.wait()

def read(p, t, d, rate=48000):
    """d seconds of a track from t seconds into its file."""
    if p.endswith(".wav"):
        off, n, bits = _wav(p); w = bits // 8
        mm = np.memmap(p, dtype=np.uint8, mode="r", offset=off, shape=(n - n % w,))
        a = int(t * rate) * w
        return _decode(np.asarray(mm[a:a + int(d * rate) * w]), bits)
    out = subprocess.run(["ffmpeg", "-nostdin", "-loglevel", "error", "-ss", f"{t:.4f}", "-t", f"{d:.4f}", "-i", p,
                          "-f", "f32le", "-ac", "1", "-ar", str(rate), "-"], capture_output=True, check=True).stdout
    return np.frombuffer(out, "<f4").copy()

if __name__ == "__main__" and len(sys.argv) == 3 and sys.argv[1] == "get":
    print(setting(sys.argv[2]) or "")
