# The concert as a console video: the strips (fader, mute, meter, EQ,
# compressor) as they moved during the show, a chat telling each change, a
# panel drawing the EQ, compressor or effect being changed, and the song.
#
#   render.py still OUT.png VIDEO_SECONDS     one frame
#   render.py frames FIRST COUNT              raw BGRx frames on stdout
#
# Run from the project folder. Reads project.json (titles, the FOH tracks,
# the effects, what is shown), analysis/segments.json ({"sets": [{"name",
# "a", "b"}], "chapters": [{"t", "title"}]}: the parts of each set shown, in
# session seconds, and the chapters in video seconds), and per set:
# <set>-state0.json (the console at the start), the session's events.jsonl,
# <set>-meters30.npz, <set>-gestures.json.
import json, math, re, sys, os
import numpy as np
import cairo, gi
gi.require_version("Pango", "1.0"); gi.require_version("PangoCairo", "1.0")
from gi.repository import Pango, PangoCairo
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, labels

CFG = project(); P = CFG["dir"]
A = f"{P}/analysis"
FOH = CFG.get("foh", {"tracks": [23, 24], "matrix": 1, "label": "FOH"})
FX = CFG.get("fx", [])
DELAY_FX = CFG.get("delay_fx")
W, H, FPS = 2560, 1072, 30

# ---- palette and type ------------------------------------------------------
BG = (0.071, 0.078, 0.094); PANEL = (0.105, 0.115, 0.137); PANEL2 = (0.135, 0.146, 0.172)
INK = (0.91, 0.92, 0.94); DIM = (0.55, 0.58, 0.64); FAINT = (0.30, 0.32, 0.37)
AMBER = (0.96, 0.70, 0.24); RED = (0.90, 0.27, 0.27); GREEN = (0.30, 0.78, 0.42); YELLOW = (0.96, 0.84, 0.25)
RTA_FILL = (0.27, 0.52, 0.72); RTA_LINE = (0.55, 0.80, 0.95)  # the spectrum behind the EQ, and after it
RTA_LO, RTA_HI = -100.0, -20.0  # dB per band drawn from the bottom to the top of an EQ box
COLORS = {"RD": (0.90, 0.29, 0.30), "GN": (0.30, 0.70, 0.38), "YE": (0.96, 0.85, 0.20), "BL": (0.27, 0.43, 0.90),
          "MG": (0.84, 0.30, 0.65), "CY": (0.10, 0.65, 0.78), "WH": (0.88, 0.88, 0.88), "OFF": (0.35, 0.36, 0.40)}
SANS, COND, MONO = "Noto Sans", "DejaVu Sans Condensed", "Noto Sans Mono"

# Text is drawn once into a small image, then pasted: Pango on every frame
# costs more than the rest of the frame.
_texts = {}
def text(cr, x, y, s, size, color=INK, font=SANS, weight="", align="left", width=None, alpha=1.0, wrap=False):
    key = (s, size, color, font, weight, align, width, wrap)
    t = _texts.get(key)
    if t is None:
        tmp = cairo.ImageSurface(cairo.FORMAT_ARGB32, 4, 4); tc = cairo.Context(tmp)
        lay = PangoCairo.create_layout(tc)
        lay.set_font_description(Pango.FontDescription(f"{font} {weight} {size}px"))
        lay.set_text(s, -1)
        if width:
            lay.set_width(int(width * Pango.SCALE))
            lay.set_wrap(Pango.WrapMode.WORD_CHAR) if wrap else lay.set_ellipsize(Pango.EllipsizeMode.END)
            lay.set_alignment({"left": Pango.Alignment.LEFT, "center": Pango.Alignment.CENTER, "right": Pango.Alignment.RIGHT}[align])
        ink, log = lay.get_pixel_extents()
        x0 = min(0, ink.x, log.x); w = max(log.x + log.width, ink.x + ink.width, 1) - x0 + 2
        y0 = min(0, ink.y, log.y); h = max(log.y + log.height, ink.y + ink.height, 1) - y0 + 2
        surf = cairo.ImageSurface(cairo.FORMAT_ARGB32, int(w), int(h)); c2 = cairo.Context(surf)
        c2.set_source_rgb(*color); c2.move_to(-x0, -y0); PangoCairo.show_layout(c2, lay)
        if len(_texts) > 30000: _texts.clear()
        t = _texts[key] = (surf, x0, y0, log.width, log.height)
    surf, x0, y0, lw, lh = t
    if not width and align != "left":
        x -= lw if align == "right" else lw / 2
    cr.set_source_surface(surf, round(x + x0), round(y + y0))
    cr.paint_with_alpha(alpha) if alpha < 1 else cr.paint()
    return lw, lh

def rrect(cr, x, y, w, h, r):
    cr.new_sub_path(); cr.arc(x + w - r, y + r, r, -math.pi/2, 0); cr.arc(x + w - r, y + h - r, r, 0, math.pi/2)
    cr.arc(x + r, y + h - r, r, math.pi/2, math.pi); cr.arc(x + r, y + r, r, math.pi, 1.5*math.pi); cr.close_path()

# ---- values ------------------------------------------------------------------
def db_text(t):
    t = t.strip()
    if t in ("-oo", "-inf"): return -math.inf
    try: return float(t)
    except ValueError: return None

def hz(t):
    t = t.strip()
    if "k" in t: a, b = t.split("k"); return float(a) * 1000 + (float("0." + b) * 1000 if b else 0)
    return float(t)

def level_of(v):  # fader law, normalized → dB
    if v <= 0: return -math.inf
    return 480*v - 90 if v < 0.0625 else 160*v - 70 if v < 0.25 else 80*v - 50 if v < 0.5 else 40*v - 30

def pos_of(dbv):  # dB → normalized fader position
    if dbv == -math.inf or dbv <= -90: return 0.0
    return (dbv + 90)/480 if dbv < -60 else (dbv + 70)/160 if dbv < -30 else (dbv + 50)/80 if dbv < -10 else (dbv + 30)/40

# ---- EQ and compressor drawing maths -----------------------------------------
FS = 48000.0
FREQS = np.geomspace(20, 20000, 220)
def biquad_db(b, a):
    w = 2*np.pi*FREQS/FS; z = np.exp(-1j*w)
    h = (b[0] + b[1]*z + b[2]*z*z)/(a[0] + a[1]*z + a[2]*z*z)
    return 20*np.log10(np.maximum(np.abs(h), 1e-9))

def band_db(typ, f, g, q):
    w0 = 2*math.pi*min(f, 20000)/FS; c, s = math.cos(w0), math.sin(w0); al = s/(2*max(q, 0.1)); A_ = 10**(g/40)
    if typ in ("PEQ", "VEQ"):
        return biquad_db([1 + al*A_, -2*c, 1 - al*A_], [1 + al/A_, -2*c, 1 - al/A_])
    if typ in ("LShv", "HShv"):
        sq = 2*math.sqrt(A_)*al
        if typ == "LShv":
            return biquad_db([A_*((A_+1)-(A_-1)*c+sq), 2*A_*((A_-1)-(A_+1)*c), A_*((A_+1)-(A_-1)*c-sq)],
                             [(A_+1)+(A_-1)*c+sq, -2*((A_-1)+(A_+1)*c), (A_+1)+(A_-1)*c-sq])
        return biquad_db([A_*((A_+1)+(A_-1)*c+sq), -2*A_*((A_-1)+(A_+1)*c), A_*((A_+1)+(A_-1)*c-sq)],
                         [(A_+1)-(A_-1)*c+sq, 2*((A_-1)-(A_+1)*c), (A_+1)-(A_-1)*c-sq])
    if typ == "LCut":
        return biquad_db([(1+c)/2, -(1+c), (1+c)/2], [1+al, -2*c, 1-al])
    if typ == "HCut":
        return biquad_db([(1-c)/2, 1-c, (1-c)/2], [1+al, -2*c, 1-al])
    return np.zeros_like(FREQS)

def lowcut_db(f, slope):
    out = np.zeros_like(FREQS); w0 = 2*math.pi*f/FS; c, s = math.cos(w0), math.sin(w0)
    qs = {12: [0.7071], 18: [1.0], 24: [0.5412, 1.3066]}.get(slope, [0.7071])
    for q in qs:
        al = s/(2*q); out += biquad_db([(1+c)/2, -(1+c), (1+c)/2], [1+al, -2*c, 1-al])
    if slope == 18:
        k = math.tan(w0/2); out += biquad_db([1/(1+k), -1/(1+k), 0], [1, (k-1)/(k+1), 0])
    return out

# ---- the show ---------------------------------------------------------------------
SEG = json.load(open(f"{A}/segments.json"))
class Set:
    def __init__(self, spec):
        self.name, self.a, self.b = spec["name"], spec["a"], spec["b"]
        self.st0 = {}
        for n in json.load(open(f"{A}/{self.name}-state0.json")):
            for v in n.get("values", []): self.st0[v["addr"]] = (v["text"], v["value"])
        self.events = []
        for line in open(os.path.join(CFG["sessions"], self.name, "events.jsonl")):
            e = json.loads(line)
            if "addr" in e: self.events.append((e["t"], e["addr"], e["text"], e["value"]))
        self.events.sort(key=lambda e: e[0])
        m = np.load(f"{A}/{self.name}-meters30.npz"); self.moff = float(m["offset"])
        self.pk = {int(k[3:]): m[k].astype(np.float32) for k in m.files if k.startswith("pk_")}
        self.gestures = json.load(open(f"{A}/{self.name}-gestures.json"))
        mt = json.load(open(os.path.join(CFG["sessions"], self.name, "meta.json")))["audio"]["tracks"]
        self.track_names = {t["channel"]: t["name"] for t in mt}
        # the spectra (rta.py), read track by track when first drawn
        self._rta, self._rta_cache = None, {}
        if os.path.exists(f"{A}/{self.name}-rta.npz"):
            self._rta = np.load(f"{A}/{self.name}-rta.npz"); self.roff = float(self._rta["offset"])
            global RTA_BANDS
            RTA_BANDS = self._rta["bands"].astype(np.float64)
    def rta(self, ch):
        if self._rta is None or f"rta_{ch}" not in self._rta.files: return None
        if ch not in self._rta_cache: self._rta_cache[ch] = self._rta[f"rta_{ch}"]
        return self._rta_cache[ch]
RTA_BANDS = None
SETS = [Set(s) for s in SEG["sets"]]
STARTS = []; acc = 0.0
for s in SETS: STARTS.append(acc); acc += s.b - s.a
DURATION = acc
CHAPTERS = SEG.get("chapters", [])

def where(tv):
    for i in range(len(SETS) - 1, -1, -1):
        if tv >= STARTS[i]: return i, SETS[i].a + (tv - STARTS[i])
    return 0, SETS[0].a

# ---- strips ------------------------------------------------------------------
# what was muted for the whole of the parts shown is left out when nothing of it was recorded
def muted_throughout(addr_on, sets):
    for s in sets:
        if s.st0.get(addr_on, ("OFF", 0))[0].strip() == "ON": return False
        for t, a, txt, _ in s.events:
            if a == addr_on and txt.strip() == "ON" and t < s.b: return False
    return True

def audible_sometime(fader, on, sets):
    """Whether the strip was on with its fader up at some moment of the parts shown."""
    for s in sets:
        v = {fader: s.st0.get(fader, ("", 0.0))[1], on: s.st0.get(on, ("OFF", 0))[0].strip()}
        def up(): return v[on] == "ON" and float(v[fader] or 0) > 0
        crossed = False
        for t, a, txt, val in s.events:
            if t > s.b: break
            if t >= s.a and not crossed:
                crossed = True
                if up(): return True  # as it was when the part starts
            if a == fader: v[a] = val
            elif a == on: v[a] = txt.strip()
            else: continue
            if t >= s.a and up(): return True
        if up(): return True
    return False

def silent(track):
    return all(track not in s.pk or float(np.max(s.pk[track][max(0, int((s.a - s.moff)*FPS)):int((s.b - s.moff)*FPS)])) < -100 for s in SETS)

def input_list(s):
    """The input channels shown: project.json "inputs", or every channel with a recorded track, and every
    named channel going to the main mix that was on with its fader up at some time."""
    if CFG.get("inputs"): return CFG["inputs"]
    t = lambda a: s.st0.get(a, ("", 0))[0].strip().strip('"')
    out = []
    for n in range(1, 33):
        p = f"/ch/{n:02d}"
        if track_of(s, n): out.append(n); continue
        if t(p + "/config/name") in ("", "-") or t(p + "/mix/st") != "ON": continue
        if audible_sometime(p + "/mix/fader", p + "/mix/on", SETS): out.append(n)
    return out

def track_of(s, n):
    """The card track recording channel n: project.json "input_tracks", or the one track tabula named after it."""
    if str(n) in CFG.get("input_tracks", {}): return CFG["input_tracks"][str(n)]
    name = s.st0.get(f"/ch/{n:02d}/config/name", ("", 0))[0].strip().strip('"')
    same = [c for c, nm in s.track_names.items() if nm == name and name not in ("", "-")]
    return same[0] if len(same) == 1 else None

def strip_defs(s):
    st = s.st0; t = lambda a: st.get(a, ("", 0))[0].strip().strip('"')
    out = []
    def col(c): return COLORS.get(c.rstrip("i"), COLORS["WH"])
    for n in input_list(s):
        p = f"/ch/{n:02d}"; tr = track_of(s, n)
        out.append(dict(key=f"ch{n}", group="INPUTS", num=str(n), name=t(p + "/config/name"), color=col(t(p + "/config/color")),
                        fader=p + "/mix/fader", mute=p + "/mix/on", meter=[tr] if tr else None, eq=p + "/eq", hpf=p + "/preamp", dyn=p + "/dyn"))
    for f in FX:
        k = f["fx"]; p = f"/fxrtn/{f.get('return', 2*k - 1):02d}"; tr = f.get("send_track")
        out.append(dict(key=f"fx{k}", group="EFFECTS", num=f"FX{k}", name=f.get("short", f"FX {k}"), color=col(t(p + "/config/color")),
                        fader=p + "/mix/fader", mute=p + "/mix/on", meter=[tr] if tr else None, eq=p + "/eq", fx=k, meterlabel="send" if tr else None))
    for k in range(1, 9):
        p = f"/dca/{k}"
        if t(p + "/config/name") in ("", "-") or k in CFG.get("dca_hidden", []): continue
        out.append(dict(key=f"dca{k}", group="DCA", num=f"DCA{k}", name=t(p + "/config/name"), color=col(t(p + "/config/color")),
                        fader=p + "/fader", mute=p + "/on", meter=None))
    fm = FOH.get("matrix")
    out.append(dict(key="main", group="OUTPUTS", num="LR", name="Main", color=COLORS["WH"], fader="/main/st/mix/fader",
                    mute="/main/st/mix/on", meter="main" if fm else FOH["tracks"], eq="/main/st/eq", dyn="/main/st/dyn"))
    for m, label, meter in ([(fm, FOH.get("label", "FOH"), FOH["tracks"])] if fm else []) + \
                           [(o["matrix"], o["label"], ("matrix", o["matrix"])) for o in CFG.get("outputs", [])]:
        p = f"/mtx/{m:02d}"
        out.append(dict(key=f"mtx{m}", group="OUTPUTS", num=f"MX{m}-{m + 1}", name=label, color=col(t(p + "/config/color")),
                        fader=p + "/mix/fader", mute=p + "/mix/on", meter=meter))
    return out

STRIPS = strip_defs(SETS[0])
HIDDEN = set()
for st in STRIPS:
    if st["key"].startswith("fx") and (muted_throughout(st["mute"], SETS) or (st["meter"] and silent(st["meter"][0]))):
        HIDDEN.add(st["key"])
STRIPS = [s for s in STRIPS if s["key"] not in HIDDEN]
VISIBLE = {s["key"] for s in STRIPS}

# ---- layout ----------------------------------------------------------------------
TOP = 66; BOT = H - 30; CX0, CX1 = 22, 1712; RX0, RX1 = 1736, W - 22  # BOT: where the panels end, the progress below
groups = []
for s in STRIPS:
    if not groups or groups[-1][0] != s["group"]: groups.append([s["group"], []])
    groups[-1][1].append(s)
GAPG = 18
SW = (CX1 - CX0 - GAPG*(len(groups)-1)) / len(STRIPS)
x = CX0
for g, ss in groups:
    for s in ss: s["x"] = x; x += SW
    x += GAPG
Y_HEAD, Y_SCR, Y_EQ, Y_DYN, Y_MUTE, Y_FAD0, Y_FAD1 = TOP + 10, TOP + 34, TOP + 96, TOP + 152, TOP + 178, TOP + 226, BOT - 54
FADER_H = Y_FAD1 - Y_FAD0

# ---- state through time ------------------------------------------------------------
class Live:
    def __init__(self):
        self.si = None
    def at(self, tv):
        si, ts = where(tv); s = SETS[si]
        if si != self.si or ts < self.ts:
            self.si, self.ts, self.i = si, -1e9, 0
            self.v = dict(s.st0)
        while self.i < len(s.events) and s.events[self.i][0] <= ts:
            _, a, txt, val = s.events[self.i]; self.v[a] = (txt, val); self.i += 1
        self.ts = ts
        return s, ts
    def text(self, a, d=""):
        return self.v.get(a, (d, 0))[0]
    def val(self, a, d=0.0):
        return self.v.get(a, ("", d))[1]

# ---- meters with ballistics ------------------------------------------------------------
class Meters:
    def __init__(self): self.disp = {}; self.hold = {}; self.last = None; self.spec = {}
    def rta(self, s, ts, ch, key, tv):
        """The spectrum of track ch now, falling at most 1.5 dB a frame (None without one)."""
        arr = s.rta(ch)
        if arr is None: return None
        i = int((ts - s.roff) * FPS)
        if i < 0 or i >= len(arr): return None
        now = arr[i].astype(np.float32); prev = self.spec.get(key)
        fresh = prev is None or self.last is None or abs(tv - self.last) > 0.2
        d = now if fresh else np.maximum(now, prev - 1.5)
        self.spec[key] = d
        return d
    def level(self, s, ts, ch):
        arr = s.pk.get(ch)
        if arr is None: return -120.0
        i = int((ts - s.moff) * FPS)
        if i < 0 or i >= len(arr): return -120.0
        return float(arr[i])
    def get(self, key, now, tv):
        d0 = self.disp.get(key, -120.0)
        fresh = self.last is None or abs(tv - self.last) > 0.2
        d = now if fresh else max(now, d0 - 0.9)
        self.disp[key] = d
        h, ht = self.hold.get(key, (-120.0, tv))
        if now >= h or tv - ht > 1.5: h, ht = (now, tv) if now >= h - 0.01 else (max(now, h - 1.2), ht)
        self.hold[key] = (h, ht)
        return d, h

def meter_color(cr, d):
    cr.set_source_rgb(*(RED if d > -3 else YELLOW if d > -12 else GREEN))

# ---- chat and focus from the gestures ----------------------------------------------
MSGS = []
for si, s in enumerate(SETS):
    for g in s.gestures:
        if not (s.a <= g["t0"] <= s.b): continue
        g = dict(g); g["tv0"] = STARTS[si] + g["t0"] - s.a; g["tv1"] = STARTS[si] + g["t1"] - s.a; g["set"] = si
        if "strips" in g:  # a group of mutes: only the strips on screen
            keep = [i for i, k in enumerate(g["strips"]) if k in VISIBLE]
            if not keep: continue
            names = [g["group"][i] for i in keep]
            verb = g["intent"].split()[0]
            names = list(dict.fromkeys(names))
            g["intent"] = f"{verb} " + (names[0] if len(names) == 1 else f"{len(names)} strips: " + ", ".join(n.replace("the ", "") for n in names))
            g["detail"] = ", ".join(dict.fromkeys(g["details"][i].rsplit(" ", 1)[0] for i in keep)) + (" muted" if verb == "Mute" else " on")
            g["strip"] = g["strips"][keep[0]]
        elif g["strip"] and g["strip"] not in VISIBLE:
            continue
        MSGS.append(g)
MSGS.sort(key=lambda g: g["tv0"])
STRIP_BY = {s["key"]: s for s in STRIPS}

def live_detail(g, lv):
    """The gesture's detail, its numbers following the control while it moves."""
    if lv.tvn >= g["tv1"] or len(g["after"]) != 1: return g["detail"]
    a = next(iter(g["after"])); final = g["after"][a].strip(); now = lv.text(a, final).strip()
    if now == final: return g["detail"]
    d = g["detail"]; i = d.rfind(final.replace("-", "−")) if final.replace("-", "−") in d else d.rfind(final)
    if i < 0: return d
    nowf = now
    try:
        if now not in ("-oo",) and re.match(r"^[+-]?\d", now) and ("dB" in d): nowf = f"{float(now):+.1f}"
    except ValueError: pass
    if now == "-oo": nowf = "−∞"
    return d[:i] + nowf.replace("-", "−") + d[i + len(final.replace('-', '−') if final.replace('-', '−') in d else final):]

# ---- drawing ------------------------------------------------------------------------
_bg = None
def background():
    global _bg
    if _bg: return _bg
    surf = cairo.ImageSurface(cairo.FORMAT_RGB24, W, H); cr = cairo.Context(surf)
    cr.set_source_rgb(*BG); cr.paint()
    # console panel
    cr.set_source_rgb(*PANEL); rrect(cr, CX0 - 10, TOP, CX1 - CX0 + 20, BOT - TOP, 10); cr.fill()
    for g, ss in groups:
        x0, x1 = ss[0]["x"], ss[-1]["x"] + SW
        text(cr, x0 + 4, Y_HEAD, g, 13, DIM, SANS, "Bold")
        cr.set_source_rgb(*FAINT); cr.rectangle(x0 + 4, Y_HEAD + 19, x1 - x0 - 8, 1); cr.fill()
    for s in STRIPS:
        x = s["x"]; cx = x + SW/2
        # fader scale
        for dbv, lab in ((10, "+10"), (5, "+5"), (0, "0"), (-5, "−5"), (-10, "−10"), (-20, "−20"), (-30, "−30"), (-40, "−40"), (-50, "−50"), (-90, "−∞")):
            y = Y_FAD1 - pos_of(dbv)*FADER_H
            cr.set_source_rgb(*FAINT); cr.rectangle(x + 8, y, 9, 1); cr.fill()
            text(cr, x + 6, y - 7, lab, 10, DIM, COND, align="right") if False else text(cr, x + 4, y - 15, lab, 10, FAINT, COND)
        cr.set_source_rgb(0.05, 0.05, 0.06); rrect(cr, cx - 3, Y_FAD0 - 4, 6, FADER_H + 8, 3); cr.fill()
    # right column
    cr.set_source_rgb(*PANEL); rrect(cr, RX0, TOP, RX1 - RX0, 372, 10); cr.fill()
    cr.set_source_rgb(*PANEL); rrect(cr, RX0, TOP + 388, RX1 - RX0, BOT - TOP - 388, 10); cr.fill()
    text(cr, RX0 + 18, TOP + 400, "AT THE DESK", 13, DIM, SANS, "Bold")
    _bg = surf
    return surf

def eq_curve(lv, prefix, hpf=None, ghost=None):
    bands = []
    for b in range(1, 7):
        tt = lv.text(f"{prefix}/{b}/type")
        if not tt: break
        try: bands.append((tt.strip(), hz(lv.text(f"{prefix}/{b}/f")), float(lv.text(f"{prefix}/{b}/g")), float(lv.text(f"{prefix}/{b}/q"))))
        except ValueError: pass
    on = lv.text(f"{prefix}/on", "ON").strip() == "ON"
    key = (tuple(bands), on, hpf)
    c = _eqcache.get(key)
    if c is None:
        c = np.zeros_like(FREQS)
        if on:
            for typ, f, g, q in bands: c += band_db(typ, f, g, q)
        if hpf: c += lowcut_db(*hpf)
        if len(_eqcache) > 5000: _eqcache.clear(); _mini.clear()  # the mini images are keyed by curve
        _eqcache[key] = c
    return c, bands, on
_eqcache = {}
_mini = {}

def hpf_of(lv, strip):
    p = strip.get("hpf")
    if not p or lv.text(p + "/hpon", "OFF").strip() != "ON": return None
    try: return (float(lv.text(p + "/hpf")), int(lv.text(p + "/hpslope", "24").strip() or 24))
    except ValueError: return None

def plot_spectrum(cr, x, y, w, h, spec, color, alpha, fill=True, lw=1.5):
    xs = x + np.log10(RTA_BANDS/20)/3*w; ys = y + h - np.clip((spec - RTA_LO)/(RTA_HI - RTA_LO), 0, 1)*h
    if fill:
        cr.move_to(xs[0], y + h)
        for a, b in zip(xs, ys): cr.line_to(a, b)
        cr.line_to(xs[-1], y + h); cr.close_path(); cr.set_source_rgba(*color, alpha); cr.fill()
    else:
        cr.move_to(xs[0], ys[0])
        for a, b in zip(xs[1:], ys[1:]): cr.line_to(a, b)
        cr.set_source_rgba(*color, alpha); cr.set_line_width(lw); cr.stroke()

def plot_curve(cr, x, y, w, h, curve, rng=15, color=AMBER, lw=1.5, alpha=1.0, fill=False):
    xs = x + np.log10(FREQS/20)/3*w; ys = y + h/2 - np.clip(curve, -rng*1.6, rng*1.6)/rng*(h/2)
    cr.move_to(xs[0], ys[0])
    for a, b in zip(xs[1:], ys[1:]): cr.line_to(a, b)
    if fill:
        cr.line_to(xs[-1], y + h/2); cr.line_to(xs[0], y + h/2); cr.close_path(); cr.set_source_rgba(*color, 0.18*alpha); cr.fill()
        cr.move_to(xs[0], ys[0])
        for a, b in zip(xs[1:], ys[1:]): cr.line_to(a, b)
    cr.set_source_rgba(*color, alpha); cr.set_line_width(lw); cr.stroke()

def active_strips(tv):
    act = {}
    for g in MSGS:
        if g["tv0"] - 0.05 <= tv <= g["tv1"] + 1.2:
            for k in g.get("strips", [g["strip"]]): act[k] = g["cat"]
    return act

def draw_strips(cr, lv, s, ts, tv, meters):
    act = active_strips(tv)
    fm = FOH.get("matrix"); foh_level = level_of(lv.val(f"/mtx/{fm:02d}/mix/fader")) if fm else 0.0
    for st in STRIPS:
        x = st["x"]; cx = x + SW/2; k = st["key"]
        if k in act:
            cr.set_source_rgba(*AMBER, 0.13); rrect(cr, x + 2, Y_SCR - 6, SW - 4, Y_FAD1 + 48 - Y_SCR, 7); cr.fill()
        # scribble strip
        cr.set_source_rgb(*st["color"]); rrect(cr, x + 4, Y_SCR, SW - 8, 54, 5); cr.fill()
        dark = sum(st["color"]) > 1.9
        text(cr, x + 4, Y_SCR + 3, st["num"], 11, (0.1, 0.1, 0.1) if dark else (1, 1, 1), COND, "Bold", "center", SW - 8, 0.8)
        nm = st["name"]; two = len(nm) > 8 and " " in nm
        text(cr, x + 4, Y_SCR + (15 if two else 19), nm, 14 if two else 15, (0.05, 0.05, 0.05) if dark else (1, 1, 1), COND, "Bold", "center", SW - 8, wrap=two)
        # mini EQ
        if st.get("eq"):
            curve, _, on = eq_curve(lv, st["eq"], hpf_of(lv, st))
            cr.set_source_rgb(0.06, 0.065, 0.08); rrect(cr, x + 4, Y_EQ, SW - 8, 48, 4); cr.fill()
            cr.save(); rrect(cr, x + 4, Y_EQ, SW - 8, 48, 4); cr.clip()
            spec = meters.rta(s, ts, st["meter"][0], k, tv) if k.startswith("ch") and st.get("meter") else None
            if spec is not None: plot_spectrum(cr, x + 4, Y_EQ, SW - 8, 48, spec, RTA_FILL, 0.55)
            if on or hpf_of(lv, st):
                hot = act.get(k) in ("eq", "hpf"); ck = (id(curve), hot)
                img = _mini.get(ck)
                if img is None:
                    img = cairo.ImageSurface(cairo.FORMAT_ARGB32, int(SW - 8), 48); c2 = cairo.Context(img)
                    plot_curve(c2, 0, 0, SW - 8, 48, curve, 18, AMBER if hot else (0.62, 0.66, 0.74), 1.4, 1.0, fill=True)
                    if len(_mini) > 4000: _mini.clear()
                    _mini[ck] = img
                cr.set_source_surface(img, x + 4, Y_EQ); cr.paint()
            else:
                text(cr, x + 4, Y_EQ + 16, "EQ off", 11, FAINT, COND, align="center", width=SW - 8)
            cr.restore()
        # compressor
        if st.get("dyn") and lv.text(st["dyn"] + "/on").strip() == "ON":
            thr = lv.text(st["dyn"] + "/thr").strip(); rat = lv.text(st["dyn"] + "/ratio").strip()
            text(cr, x + 4, Y_DYN, f"{thr} · {rat}:1", 11, AMBER if act.get(k) == "dyn" else DIM, COND, align="center", width=SW - 8)
        elif st.get("fx"):
            fxt = lv.text(f"/fx/{st['fx']}/type").strip()
            text(cr, x + 4, Y_DYN, fxt, 11, AMBER if act.get(k) == "fx" else DIM, COND, align="center", width=SW - 8)
        # mute
        on = lv.text(st["mute"], "ON").strip() == "ON"
        def mute_button():
            cr.set_source_rgb(*(RED if not on else (0.20, 0.21, 0.24))); rrect(cr, x + 10, Y_MUTE, SW - 20, 26, 4); cr.fill()
            text(cr, x + 10, Y_MUTE + 4, "MUTE", 12, (1, 1, 1) if not on else DIM, COND, "Bold", "center", SW - 20)
        mute_button()
        # meter
        m = st.get("meter")
        if m:
            lvls = []
            # Main and the other matrices have no track: the FOH tracks, less the FOH matrix's level,
            # plus theirs (both fed by the main mix)
            if m == "main": lvls = [meters.level(s, ts, c) - foh_level for c in FOH["tracks"]]
            elif m[0] == "matrix": lvls = [meters.level(s, ts, c) - foh_level + level_of(lv.val(f"/mtx/{m[1]:02d}/mix/fader")) for c in FOH["tracks"]]
            else: lvls = [meters.level(s, ts, c) for c in m]
            mw = 7 if len(lvls) == 1 else 5
            for j, lvl in enumerate(lvls):
                d, hd = meters.get((k, j), lvl, tv)
                mx = x + SW - 14 - (len(lvls) - 1 - j)*(mw + 2)
                cr.set_source_rgb(0.04, 0.04, 0.05); cr.rectangle(mx, Y_FAD0, mw, FADER_H); cr.fill()
                frac = max(0.0, min(1.0, (d + 60)/60))
                if frac > 0:
                    yb = Y_FAD1; yt = Y_FAD1 - frac*FADER_H
                    for lo, hi, colr in ((-60, -12, GREEN), (-12, -3, YELLOW), (-3, 0, RED)):
                        a_ = Y_FAD1 - (lo + 60)/60*FADER_H; b_ = Y_FAD1 - (hi + 60)/60*FADER_H
                        top = max(b_, yt)
                        if top < a_: cr.set_source_rgb(*colr); cr.rectangle(mx, top, mw, a_ - top); cr.fill()
                hf = max(0.0, min(1.0, (hd + 60)/60))
                if hf > 0.02: meter_color(cr, hd); cr.rectangle(mx, Y_FAD1 - hf*FADER_H - 1, mw, 2); cr.fill()
            if st.get("meterlabel"): text(cr, x + SW - 30, Y_FAD1 + 4, st["meterlabel"], 9, FAINT, COND)
        # fader knob
        v = lv.val(st["fader"]); y = Y_FAD1 - v*FADER_H
        cr.set_source_rgb(0.80, 0.81, 0.84) if k not in act else cr.set_source_rgb(*AMBER)
        rrect(cr, cx - 15, y - 9, 30, 18, 3); cr.fill()
        cr.set_source_rgb(0.15, 0.15, 0.17); cr.rectangle(cx - 13, y - 1, 26, 2); cr.fill()
        lvl = level_of(v)
        text(cr, x, Y_FAD1 + 14, "−∞" if lvl == -math.inf else "0.0" if abs(lvl) < 0.05 else f"{lvl:+.1f}".replace("-", "−"), 14,
             AMBER if k in act else INK, MONO, align="center", width=SW)
        if not on and k not in act:
            cr.rectangle(x + 1, Y_SCR - 6, SW - 2, Y_FAD1 + 40 - Y_SCR)
            cr.set_operator(cairo.OPERATOR_HSL_SATURATION); cr.set_source_rgb(0.5, 0.5, 0.5); cr.fill_preserve()
            cr.set_operator(cairo.OPERATOR_OVER); cr.set_source_rgba(*PANEL, 0.55); cr.fill()
            mute_button()

def draw_top(cr, tv):
    tw, _ = text(cr, CX0, 18, CFG.get("title", "").upper(), 26, INK, SANS, "Bold")
    text(cr, CX0 + tw + 22, 24, CFG.get("subtitle", ""), 19, DIM, SANS)
    ch = None
    for c in CHAPTERS:
        if c["t"] <= tv + 1e-6: ch = c
    if ch: text(cr, 980, 14, ch["title"], 30, AMBER, SANS, "Bold", "center", 1100)
    hh = int(tv // 3600); mm = int(tv % 3600 // 60); ss = int(tv % 60)
    text(cr, RX1, 20, f"{hh:d}:{mm:02d}:{ss:02d}", 24, INK, MONO, align="right")

def draw_progress(cr, tv):
    # the concert's progress with the chapters, under the panels
    y = BOT + 13; cr.set_source_rgb(*FAINT); cr.rectangle(CX0, y, RX1 - CX0, 3); cr.fill()
    cr.set_source_rgb(*AMBER); cr.rectangle(CX0, y, (RX1 - CX0)*tv/DURATION, 3); cr.fill()
    for c in CHAPTERS:
        xx = CX0 + (RX1 - CX0)*c["t"]/DURATION; cr.set_source_rgb(*INK); cr.rectangle(xx, y - 3, 1.5, 9); cr.fill()

def focus_gesture(tv):
    best = None
    for g in MSGS:
        if g["cat"] in ("eq", "hpf", "dyn", "fx", "send", "gain", "out") and g["tv0"] <= tv <= g["tv1"] + 7:
            best = g
    return best

def draw_focus(cr, lv, tv):
    x0, y0, w, h = RX0 + 18, TOP + 14, RX1 - RX0 - 36, 344
    g = focus_gesture(tv)
    if not g:
        text(cr, x0, y0, "DETAIL", 13, DIM, SANS, "Bold")
        text(cr, x0, y0 + 150, "The EQ, compressor or effect being changed shows here.", 17, FAINT, SANS, align="center", width=w)
        return
    fade = 1.0 if tv <= g["tv1"] + 5.5 else max(0.0, (g["tv1"] + 7 - tv)/1.5)
    st = STRIP_BY.get(g["strip"]); col = st["color"] if st else AMBER
    cr.set_source_rgba(*col, fade); rrect(cr, x0, y0 + 3, 6, 18, 2); cr.fill()
    text(cr, x0 + 14, y0, g["intent"], 18, INK, SANS, "Bold", width=w - 14, alpha=fade)
    gx, gy, gw, gh = x0, y0 + 40, w, h - 60
    cat = g["cat"]
    if cat in ("eq", "hpf") and st and st.get("eq"):
        cr.set_source_rgba(0.06, 0.065, 0.08, fade); rrect(cr, gx, gy, gw, gh, 6); cr.fill()
        for f in (50, 100, 200, 500, 1000, 2000, 5000, 10000):
            xx = gx + math.log10(f/20)/3*gw; cr.set_source_rgba(*FAINT, 0.6*fade); cr.rectangle(xx, gy, 1, gh); cr.fill()
            text(cr, xx + 3, gy + gh - 18, f"{f//1000}k" if f >= 1000 else str(f), 11, FAINT, COND, alpha=fade)
        for dbv in (-12, -6, 0, 6, 12):
            yy = gy + gh/2 - dbv/15*(gh/2); cr.set_source_rgba(*FAINT, (0.9 if dbv == 0 else 0.4)*fade); cr.rectangle(gx, yy, gw, 1); cr.fill()
            text(cr, gx + 4, yy - 15, f"{dbv:+d}".replace("+0", "0").replace("-", "−"), 11, FAINT, COND, alpha=fade)
        # the curve before the gesture, as a ghost
        ghost = Live(); ghost.v = dict(lv.v)
        for a, b in g["before"].items(): ghost.v[a] = (b, ghost.v.get(a, ("", 0))[1])
        gc, _, _ = eq_curve(ghost, st["eq"], hpf_of(ghost, st))
        cur, bands, on = eq_curve(lv, st["eq"], hpf_of(lv, st))
        cr.save(); rrect(cr, gx, gy, gw, gh, 6); cr.clip()
        spec = METERS.spec.get(g["strip"]) if g["strip"].startswith("ch") else None
        if spec is not None and RTA_BANDS is not None:
            after = spec + np.interp(np.log10(RTA_BANDS), np.log10(FREQS), cur)
            ry, rh = gy + 0.2*gh, 0.8*gh  # under the EQ's own scale, which keeps the top
            plot_spectrum(cr, gx, ry, gw, rh, spec, RTA_FILL, 0.40*fade)
            plot_spectrum(cr, gx, ry, gw, rh, after, RTA_LINE, 0.85*fade, fill=False, lw=1.6)
        plot_curve(cr, gx, gy, gw, gh, gc, 15, DIM, 1.5, 0.7*fade)
        plot_curve(cr, gx, gy, gw, gh, cur, 15, col if sum(col) < 2.6 else AMBER, 2.6, fade, fill=True)
        # the band moved
        bandn = int(g["control"][2]) if g["cat"] == "eq" else None
        for i, (typ, f, gg, q) in enumerate(bands, 1):
            xx = gx + math.log10(max(f, 20)/20)/3*gw; yy = gy + gh/2 - (gg if typ in ("PEQ", "VEQ", "LShv", "HShv") else 0)/15*(gh/2)
            r = 9 if i == bandn else 6
            cr.set_source_rgba(*(AMBER if i == bandn else DIM), fade); cr.arc(xx, yy, r, 0, 2*math.pi); cr.fill()
            text(cr, xx - 4, yy - 8, str(i), 11, BG, COND, "Bold", alpha=fade)
        cr.restore()
        if spec is not None and RTA_BANDS is not None:
            cr.set_source_rgba(*RTA_FILL, 0.8*fade); cr.rectangle(gx + gw - 330, gy + gh + 6, 14, 10); cr.fill()
            text(cr, gx + gw - 310, gy + gh + 2, "input", 13, DIM, SANS, alpha=fade)
            cr.set_source_rgba(*RTA_LINE, fade); cr.rectangle(gx + gw - 250, gy + gh + 10, 16, 2); cr.fill()
            text(cr, gx + gw - 228, gy + gh + 2, "after the EQ (RTA)", 13, DIM, SANS, alpha=fade)
    elif cat == "dyn" and st and st.get("dyn"):
        cr.set_source_rgba(0.06, 0.065, 0.08, fade); rrect(cr, gx, gy, gh + 0, gh, 6); cr.fill()
        def tf(thr, ratio, knee, x):
            if x < thr - knee/2: return x
            if knee > 0 and x < thr + knee/2: return x + (1/ratio - 1)*(x - thr + knee/2)**2/(2*knee)
            return thr + (x - thr)/ratio
        def curve_of(L):
            try: thr = float(L.text(st["dyn"] + "/thr")); ratio = float(L.text(st["dyn"] + "/ratio")); knee = float(L.text(st["dyn"] + "/knee"))
            except ValueError: return None
            return thr, ratio, knee
        ghost = Live(); ghost.v = dict(lv.v)
        for a, b in g["before"].items(): ghost.v[a] = (b, 0)
        for L, colr, lw, al in ((ghost, DIM, 1.5, 0.7), (lv, AMBER, 2.6, 1.0)):
            c = curve_of(L)
            if not c: continue
            thr, ratio, knee = c
            for i, xin in enumerate(np.linspace(-60, 0, 120)):
                xo = gx + (xin + 60)/60*gh; yo = gy + gh - (tf(thr, ratio, knee, xin) + 60)/60*gh
                cr.line_to(xo, yo) if i else cr.move_to(xo, yo)
            cr.set_source_rgba(*colr, al*fade); cr.set_line_width(lw); cr.stroke()
        c = curve_of(lv)
        if c:
            text(cr, gx + gh + 24, gy + 20, f"Threshold  {c[0]:+.1f} dB".replace("-", "−"), 20, INK, MONO, alpha=fade)
            text(cr, gx + gh + 24, gy + 54, f"Ratio      {c[1]:g}:1", 20, INK, MONO, alpha=fade)
            text(cr, gx + gh + 24, gy + 88, f"Knee       {c[2]:g}", 20, DIM, MONO, alpha=fade)
    elif cat == "fx":
        fx = int(g["control"][0][2:]) if g["control"][0].startswith("fx") else None
        par = g["control"][2] if len(g["control"]) > 2 else None
        if fx:
            rows = [(k_, v_) for k_, v_ in sorted(lv.v.items()) if k_.startswith(f"/fx/{fx}/par/")][:12]
            labels = FXLABELS.get(fx, {})
            for i, (a, (tx, vv)) in enumerate(rows):
                lab = labels.get(a)
                if not lab: continue
                yy = gy + 6 + i*24; hot = a.endswith(f"/{par}")
                text(cr, gx, yy, lab, 16, AMBER if hot else DIM, SANS, "Bold" if hot else "", alpha=fade)
                cr.set_source_rgba(0.06, 0.065, 0.08, fade); cr.rectangle(gx + 170, yy + 6, 300, 9); cr.fill()
                cr.set_source_rgba(*(AMBER if hot else (0.45, 0.48, 0.55)), fade); cr.rectangle(gx + 170, yy + 6, 300*float(vv), 9); cr.fill()
                text(cr, gx + 490, yy, tx.strip(), 16, AMBER if hot else INK, MONO, alpha=fade)
            if fx == DELAY_FX:
                try:
                    ms = float(lv.text(f"/fx/{fx}/par/01")); bpm = 60000/ms
                    text(cr, gx + 620, gy + 30, f"{bpm:.0f}", 72, AMBER, SANS, "Bold", alpha=fade)
                    text(cr, gx + 620, gy + 120, "BPM (1/4 note)", 16, DIM, SANS, alpha=fade)
                    beat = (tv * bpm/60) % 1
                    cr.set_source_rgba(*AMBER, fade*max(0.0, 1 - beat*3)); cr.arc(gx + 700, gy + 200, 26, 0, 2*math.pi); cr.fill()
                except ValueError: pass
    else:
        text(cr, gx, gy + 20, g["detail"], 22, INK, MONO, width=gw, wrap=True, alpha=fade)

def draw_chat(cr, lv, tv):
    x0, y0, w = RX0 + 18, TOP + 430, RX1 - RX0 - 36; ybot = BOT - 14
    shown = [g for g in MSGS if g["tv0"] <= tv][-14:]
    y = ybot
    # the newest slides in from below
    if shown:
        age = tv - shown[-1]["tv0"]; y += max(0.0, 1 - age/0.25)*40
    cr.save(); cr.rectangle(RX0, y0, RX1 - RX0, ybot - y0 + 10); cr.clip()
    for g in reversed(shown):
        st = STRIP_BY.get(g["strip"]); col = st["color"] if st else AMBER
        live = tv <= g["tv1"] + 0.3
        det = live_detail(g, lv)
        lay_h = 30 + (24 if len(g["intent"]) > 52 else 0) + (22 if len(det) > 62 else 0)
        y -= lay_h + 14
        if y + lay_h < y0: break
        tm = g["tv0"]; stamp = f"{int(tm//3600)}:{int(tm%3600//60):02d}:{int(tm%60):02d}"
        cr.set_source_rgb(*col); cr.arc(x0 + 6, y + 13, 6, 0, 2*math.pi); cr.fill()
        if live:
            cr.set_source_rgba(*AMBER, 0.10); rrect(cr, x0 - 6, y - 6, w + 12, lay_h + 10, 6); cr.fill()
        text(cr, x0 + 22, y, g["intent"], 19, INK, SANS, "Bold", width=w - 110, wrap=True)
        text(cr, x0 + w, y + 2, stamp, 13, FAINT, MONO, align="right")
        hh = 26 if len(g["intent"]) <= 52 else 50
        text(cr, x0 + 22, y + hh, f"({det})", 15, AMBER if live else DIM, MONO, width=w - 22, wrap=True)
    cr.restore()

FXLABELS = {}
for n in json.load(open(f"{A}/{SETS[0].name}-state0.json")):
    if n["path"].startswith("/fx/") and n.get("fields"):
        fx = int(n["path"].split("/")[2])
        for f in n["fields"]:
            if f.get("label"): FXLABELS.setdefault(fx, {})[f"{n['path']}/{f['name']}"] = f["label"]

def overlay_titles(cr, tv):
    # the opening title, and the second part
    for t0, big, small, d in [(0.0, CFG.get("title", ""), CFG.get("subtitle", ""), 7.0)] + \
                             [(STARTS[i], CFG.get("part_label", labels(CFG)["part"]).format(n=i + 1), "", 4.0) for i in range(1, len(SETS))]:
        dt = tv - t0
        if 0 <= dt < d:
            a = min(1.0, dt/0.6, (d - dt)/1.2)
            cr.set_source_rgba(0, 0, 0, 0.55*a); cr.rectangle(0, 0, W, H); cr.fill()
            text(cr, W/2, H/2 - 80, big, 96, INK, SANS, "Bold", "center", alpha=a) if False else text(cr, 0, H/2 - 90, big, 96, INK, SANS, "Bold", "center", W, alpha=a)
            if small: text(cr, 0, H/2 + 30, small, 34, DIM, SANS, align="center", width=W, alpha=a)

def fade_end(cr, tv):
    left = DURATION - tv
    if left < 3: cr.set_source_rgba(0, 0, 0, min(1.0, 1 - left/3)); cr.paint()

def frame(cr, lv, meters, tv):
    s, ts = lv.at(tv); lv.tvn = tv
    cr.set_source_surface(background(), 0, 0); cr.paint()
    draw_top(cr, tv); draw_progress(cr, tv)
    draw_strips(cr, lv, s, ts, tv, meters)
    draw_focus(cr, lv, tv)
    draw_chat(cr, lv, tv)
    overlay_titles(cr, tv)
    fade_end(cr, tv)
    meters.last = tv

if __name__ == "__main__":
    surf = cairo.ImageSurface(cairo.FORMAT_RGB24, W, H); cr = cairo.Context(surf)
    lv, meters = Live(), Meters(); METERS = meters
    if sys.argv[1] == "still":
        tv = float(sys.argv[3])
        for k in range(12, 0, -1):  # a few frames before, for the meters' fall
            frame(cr, lv, meters, max(0, tv - k/FPS))
        frame(cr, lv, meters, tv); surf.write_to_png(sys.argv[2])
    elif sys.argv[1] == "frames":
        first, count = int(sys.argv[2]), int(sys.argv[3]); out = sys.stdout.buffer
        for n in range(first, first + count):
            frame(cr, lv, meters, n/FPS); surf.flush(); out.write(surf.get_data())
