# The console changes of a set as gestures, for the chat of the video: the
# changes of one control less than GAP seconds apart are one gesture, linked
# pairs (FX returns, matrices, aux) count once. Each gesture says what the
# engineer meant first, then the change itself:
#   "Boost the presence on the electric guitar (Guitare Elec EQ 3 +1.5 → +3.0 dB at 3k20)"
# Writes analysis/<set>-gestures.json.
#   changes.py SET    (from the project folder; names from project.json)
import json, re, sys, math, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project

GAP = 2.0
CFG = project(); P = CFG["dir"]; SET = sys.argv[1]
SESS = os.path.join(CFG["sessions"], SET)

st0 = {}
labels = {}  # /fx/N/par/MM -> (label, unit)
for n in json.load(open(f"{P}/analysis/{SET}-state0.json")):
    for v in n.get("values", []):
        st0[v["addr"]] = v["text"]
    for f in n.get("fields") or []:
        if n["path"].startswith("/fx/") and f.get("label"):
            labels[f"{n['path']}/{f['name']}"] = (f["label"], (f.get("type_info") or {}).get("unit", ""))

def name_of(path):
    return st0.get(path + "/config/name", "").strip('"') or path

# How each strip is spoken of in the chat: project.json "spoken" (scribble
# name → words), else a singer's mic ("Vx Name") is "Name's voice" and
# anything else "the <name>".
def spoken(nm):
    if nm in CFG.get("spoken", {}): return CFG["spoken"][nm]
    if nm.startswith(("#", "/")): return nm
    m = re.match(r"(?i)(vx|vox|voix|voc|vocal)\s+(.+)", nm)
    if m: return f"{m.group(2)}'s voice"
    return f"the {nm.lower()}" if nm else nm
DCA_SPOKEN = CFG.get("dca_spoken", {})
FX_SPOKEN = {f["fx"]: f.get("spoken", f"FX {f['fx']}") for f in CFG.get("fx", [])}
FX_SHORT = {f["fx"]: f.get("short", f"FX {f['fx']}") for f in CFG.get("fx", [])}
BUS_FX = {f["send_bus"]: f["fx"] for f in CFG.get("fx", []) if f.get("send_bus")}  # FX send buses
SPEAKERS = {int(k): v for k, v in CFG.get("speakers", {}).items()}  # matrix pair → "the front speakers"
DELAY_FX = CFG.get("delay_fx")

def strip_of(addr):
    """(strip key, scribble name, spoken name) of an address."""
    m = re.match(r"/ch/(\d\d)", addr)
    if m:
        nm = name_of(f"/ch/{m.group(1)}"); return f"ch{int(m.group(1))}", nm, spoken(nm)
    m = re.match(r"/fxrtn/(\d\d)", addr)
    if m:
        fx = (int(m.group(1)) + 1) // 2; return f"fx{fx}", FX_SHORT.get(fx, f"FX {fx}") + " return", FX_SPOKEN.get(fx, f"FX {fx}")
    m = re.match(r"/fx/(\d)/", addr)
    if m:
        fx = int(m.group(1)); return f"fx{fx}", FX_SHORT.get(fx, f"FX {fx}"), FX_SPOKEN.get(fx, f"FX {fx}")
    m = re.match(r"/dca/(\d)", addr)
    if m:
        nm = st0.get(f"/dca/{m.group(1)}/config/name", "").strip('"'); return f"dca{m.group(1)}", f"DCA {nm}", DCA_SPOKEN.get(nm, f"the {nm.lower()}")
    m = re.match(r"/auxin/(\d\d)", addr)
    if m:
        a = int(m.group(1)); a -= (a + 1) % 2; nm = name_of(f"/auxin/{a:02d}")
        return f"aux{a}", (nm[:-1] if nm.endswith("L") else nm).strip(), CFG.get("aux_spoken", {}).get(str(a), "the playback")
    m = re.match(r"/mtx/(\d\d)", addr)
    if m:
        a = int(m.group(1)); a -= (a + 1) % 2; nm = name_of(f"/mtx/{a:02d}")
        return f"mtx{a}", nm[:-1] if nm.endswith("L") else nm, SPEAKERS.get(a, nm)
    if addr.startswith("/main/st"):
        return "main", "Main LR", "the main mix"
    m = re.match(r"/bus/(\d\d)", addr)
    if m:
        nm = name_of(f"/bus/{m.group(1)}"); return f"bus{int(m.group(1))}", nm, CFG.get("bus_spoken", {}).get(nm, nm)
    return None, addr, addr

def db(text):
    t = text.strip()
    if t in ("-oo", "-inf"): return -math.inf
    try: return float(t)
    except ValueError: return None

def hz(text):
    t = text.strip()
    if "k" in t: a, b = t.split("k"); return float(a) * 1000 + (float("0." + b) * 1000 if b else 0)
    return float(t)

def fmt_db(v):
    return "−∞" if v == -math.inf else f"{v:+.1f}".replace("-", "−")

def fmt_hz(f):
    return f"{f/1000:.2f} kHz".replace(".00 ", " ") if f >= 1000 else f"{f:.0f} Hz"

def region(f):
    return ("the sub" if f < 80 else "the low end" if f < 250 else "the low mids" if f < 800 else
            "the mids" if f < 2500 else "the presence" if f < 6000 else "the air")

def ratio_of(text):
    try: return float(text)
    except ValueError: return None

# --- read the events and group them ---------------------------------------
events = []
for line in open(f"{SESS}/events.jsonl"):
    e = json.loads(line)
    a = e.get("addr")
    if not a or a.startswith(("/-stat/tape", "/-usb", "/-stat/rtasource")): continue
    events.append(e)

def control(a):
    """The control an address belongs to: changes of one control make a gesture."""
    s, _, _ = strip_of(a)
    if re.search(r"/eq/\d/", a): return (s, "eq", re.search(r"/eq/(\d)/", a).group(1))
    if "/dyn/" in a: return (s, "dyn")
    if "/preamp/hp" in a: return (s, "hpf")
    m = re.search(r"/mix/(\d\d)/level", a)
    if m: return (s, "send", m.group(1))
    if a.endswith("/mix/fader") or re.match(r"/dca/\d/fader", a): return (s, "fader")
    if a.endswith("/mix/on") or re.match(r"/dca/\d/on", a): return (s, "mute")
    m = re.match(r"/fx/(\d)/par/(\d\d)", a)
    if m: return (s, "fxpar", m.group(2))
    return (a,)

state = dict(st0)
open_g = {}  # control -> gesture
gestures = []
for e in events:
    a, t = e["addr"], e["t"]
    # a linked pair's second side repeats the first: fold it in
    a2 = re.sub(r"/fxrtn/(\d\d)", lambda m: f"/fxrtn/{int(m.group(1)) - (int(m.group(1)) + 1) % 2:02d}", a)
    a2 = re.sub(r"/mtx/(\d\d)", lambda m: f"/mtx/{int(m.group(1)) - (int(m.group(1)) + 1) % 2:02d}", a2)
    a2 = re.sub(r"/auxin/(\d\d)", lambda m: f"/auxin/{int(m.group(1)) - (int(m.group(1)) + 1) % 2:02d}", a2)
    k = control(a2)
    g = open_g.get(k)
    if g is None or t - g["t1"] > GAP:
        g = {"t0": t, "t1": t, "control": list(k), "before": {}, "after": {}}
        open_g[k] = g; gestures.append(g)
    if a2 not in g["before"]: g["before"][a2] = state.get(a2, "")
    g["after"][a2] = e["text"]; g["t1"] = t
    g.setdefault("steps", []).append([round(t, 3), a2, e["text"]])
    state[a] = e["text"]; state[a2] = e["text"]
    g.setdefault("eqstate", {})
    m = re.match(r"(.*/eq/\d)/", a2)
    if m:  # the band's whole state after the change
        g["eqstate"] = {x: state.get(f"{m.group(1)}/{x}", "") for x in ("type", "f", "g", "q")}

# --- words -----------------------------------------------------------------
def headamp_channel(n):
    for c in range(32):
        if st0.get(f"/-ha/{c:02d}/index") == str(n): return f"/ch/{c+1:02d}"
    return None

def say(g):
    k = g["control"]; b, a = g["before"], g["after"]
    addr = next(iter(a)); s, scr, sp = strip_of(addr)
    kind = k[1] if len(k) > 1 else None
    if kind == "fader":
        x0, x1 = db(b[addr]), db(a[addr])
        if x0 is None or x1 is None or x0 == x1: return None
        verb = ("Fade out" if x1 == -math.inf else "Bring in" if x0 == -math.inf else "Bring up" if x1 > x0 else "Bring down")
        what = f"{scr} fader" if not scr.startswith("DCA") else scr
        return f"{verb} {sp}", f"{what} {fmt_db(x0)} → {fmt_db(x1)} dB", "fader"
    if kind == "mute":
        on = a[addr].strip() == "ON"
        if b[addr].strip() == a[addr].strip(): return None
        return (f"Unmute {sp}" if on else f"Mute {sp}"), f"{scr} {'on' if on else 'muted'}", "mute"
    if kind == "send":
        bus = int(k[2]); x0, x1 = db(b[addr]), db(a[addr])
        if x0 is None or x1 is None or x0 == x1: return None
        if bus in BUS_FX:
            fx = FX_SPOKEN.get(BUS_FX[bus], f"FX {BUS_FX[bus]}")
            if s and s.startswith("fx"):
                return (f"{'More' if x1 > x0 else 'Less'} of {sp} fed back into {fx}",
                        f"{scr} → {FX_SHORT.get(BUS_FX[bus])} send {fmt_db(x0)} → {fmt_db(x1)} dB", "send")
            return (f"{'More' if x1 > x0 else 'Less'} {fx.replace('the ', '')} on {sp}",
                    f"{scr} → {FX_SHORT.get(BUS_FX[bus])} send {fmt_db(x0)} → {fmt_db(x1)} dB", "send")
        busn = name_of(f"/bus/{bus:02d}")
        return (f"{'More' if x1 > x0 else 'Less'} {sp} in {busn}", f"{scr} → {busn} {fmt_db(x0)} → {fmt_db(x1)} dB", "send")
    if kind == "eq":
        band = k[2]; pre = addr.rsplit("/", 1)[0]
        ch = lambda x: (b.get(f"{pre}/{x}"), a.get(f"{pre}/{x}"))
        f0, f1 = ch("f"); g0, g1 = ch("g"); q0, q1 = ch("q"); t0, t1 = ch("type")
        es = g.get("eqstate", {}); fnow = hz(es["f"]) if es.get("f") else None
        reg = region(fnow) if fnow else "a band"
        on = f"on {sp}"
        if t1 is not None and t0 != t1:
            return f"Change the shape of EQ band {band} {on}", f"{scr} EQ {band} {t0.strip()} → {t1.strip()}", "eq"
        if g1 is not None and db(g0) != db(g1):
            x0, x1 = db(g0), db(g1)
            if x1 > x0: verb = "Boost" if x1 > 0 else "Ease the cut in"
            else: verb = "Cut" if x1 < 0 else "Ease the boost in"
            extra = f", {fmt_hz(hz(f0))} → {fmt_hz(hz(f1))}" if f1 is not None and f0 != f1 else f" at {fmt_hz(fnow)}"
            return f"{verb} {reg} {on}", f"{scr} EQ {band} {fmt_db(x0)} → {fmt_db(x1)} dB{extra}", "eq"
        if f1 is not None and f0 != f1:
            return f"Sweep {reg} {on}", f"{scr} EQ {band} {fmt_hz(hz(f0))} → {fmt_hz(hz(f1))}", "eq"
        if q1 is not None and q0 != q1:
            wide = float(q1) < float(q0)
            return f"{'Widen' if wide else 'Narrow'} the {reg.replace('the ', '')} band {on}", f"{scr} EQ {band} Q {q0.strip()} → {q1.strip()}", "eq"
        return None
    if kind == "hpf":
        if any(x.endswith("hpon") for x in a):
            x = [x for x in a if x.endswith("hpon")][0]; on = a[x].strip() == "ON"
            return f"{'Turn on' if on else 'Turn off'} the low cut on {sp}", f"{scr} low cut {'on' if on else 'off'}", "hpf"
        x = [x for x in a if x.endswith("hpf")][0]; f0, f1 = float(b[x]), float(a[x])
        if f0 == f1: return None
        return (f"{'Cut more low end on' if f1 > f0 else 'Let more low end through on'} {sp}",
                f"{scr} low cut {f0:.0f} → {f1:.0f} Hz", "hpf")
    if kind == "dyn":
        parts, intent = [], None
        for x in a:
            p = x.rsplit("/", 1)[1]; v0, v1 = b[x].strip(), a[x].strip()
            if v0 == v1: continue
            if p == "thr":
                intent = intent or (f"Compress {sp} harder" if float(v1) < float(v0) else f"Compress {sp} less")
                parts.append(f"threshold {fmt_db(float(v0))} → {fmt_db(float(v1))} dB")
            elif p == "ratio":
                intent = intent or (f"Compress {sp} harder" if ratio_of(v1) and ratio_of(v0) and ratio_of(v1) > ratio_of(v0) else f"Compress {sp} less")
                parts.append(f"ratio {v0} → {v1}")
            elif p == "mgain":
                intent = intent or f"{'More' if float(v1) > float(v0) else 'Less'} make-up gain on {sp}"
                parts.append(f"make-up {v0} → {v1} dB")
            elif p in ("attack", "release", "hold", "knee"):
                faster = float(v1) < float(v0)
                intent = intent or f"{'Faster' if faster else 'Slower'} compressor {p} on {sp}"
                parts.append(f"{p} {v0} → {v1}{' ms' if p != 'knee' else ''}")
            elif p == "on":
                intent = intent or f"{'Turn on' if v1 == 'ON' else 'Turn off'} the compressor on {sp}"; parts.append(f"comp {v1.lower()}")
            else:
                parts.append(f"{p} {v0} → {v1}")
        if not parts: return None
        return intent or f"Adjust the compressor on {sp}", f"{scr} comp " + ", ".join(parts), "dyn"
    if kind == "fxpar":
        fx = int(addr.split("/")[2]); lab, unit = labels.get(addr, (f"param {k[2]}", ""))
        v0, v1 = b[addr].strip(), a[addr].strip()
        if v0 == v1: return None
        def num(v):
            try: return hz(v) if "k" in v else float(v)
            except ValueError: return None
        n0, n1 = num(v0), num(v1)
        u = f" {unit}" if unit else ""
        if fx == DELAY_FX and lab.lower().startswith("time") and n1:
            return f"Set the delay to the song's tempo, about {60000 / n1:.0f} BPM", f"Delay time {v0} → {v1} ms", "fx"
        if lab.lower() == "decay" and n0 is not None and n1 is not None:
            return f"{'Lengthen' if n1 > n0 else 'Shorten'} {FX_SPOKEN.get(fx, f'FX {fx}')}", f"{FX_SHORT.get(fx, fx)} decay {v0} → {v1}{u}", "fx"
        if lab.lower() == "pre delay" and n0 is not None and n1 is not None:
            return f"{'More' if n1 > n0 else 'Less'} space before {FX_SPOKEN.get(fx, f'FX {fx}')}", f"{FX_SHORT.get(fx, fx)} pre-delay {v0} → {v1}{u}", "fx"
        return f"Tweak {FX_SPOKEN.get(fx, 'FX')} ({lab.lower()})", f"{FX_SHORT.get(fx, fx)} {lab.lower()} {v0} → {v1}{u}", "fx"
    m = re.match(r"/headamp/(\d+)/gain", addr)
    if m:
        c = headamp_channel(int(m.group(1))); _, scr2, sp2 = strip_of(c) if c else (None, f"headamp {m.group(1)}", f"input {m.group(1)}")
        x0, x1 = db(b[addr]), db(a[addr])
        return f"{'More' if x1 > x0 else 'Less'} gain on {sp2}", f"{scr2} preamp {fmt_db(x0)} → {fmt_db(x1)} dB", "gain"
    m = re.match(r"/outputs/main/(\d\d)/delay/(time|on)", addr)
    if m:
        n = int(m.group(1)); src = st0.get(f"/outputs/main/{n:02d}/src", "").strip()
        mx = int(src) - 19 if src.isdigit() and 20 <= int(src) <= 25 else None  # sources 20-25: matrices 1-6
        feed = name_of(f"/mtx/{mx:02d}") if mx else f"output {n}"
        spk = SPEAKERS.get(mx - (mx + 1) % 2, f"output {n}") if mx else f"output {n}"
        if m.group(2) == "on":
            on = a[addr].strip() == "ON"
            return f"{'Turn on' if on else 'Turn off'} the time alignment of {spk}", f"output {n} ({feed}) delay {'on' if on else 'off'}", "out"
        return f"Re-time {spk}", f"output {n} ({feed}) delay {b[addr].strip()} → {a[addr].strip()} ms", "out"
    m = re.match(r"/config/mute/(\d)", addr)
    if m:
        on = a[addr].strip() == "ON"
        return (f"{'Engage' if on else 'Release'} mute group {m.group(1)}", f"mute group {m.group(1)} {'on' if on else 'off'}", "mute")
    m = re.match(r"/-stat/solosw/(\d\d)", addr)
    if m:
        n = int(m.group(1)); on = a[addr].strip() == "ON"
        nm = name_of(f"/ch/{n:02d}") if n <= 32 else f"#{n}"
        return f"{'Listen to' if on else 'Stop listening to'} {spoken(nm)} in the headphones", f"{nm} solo {'on' if on else 'off'}", "solo"
    return None

out = []
for g in gestures:
    r = say(g)
    if r is None: continue
    intent, detail, cat = r
    s = strip_of(next(iter(g["after"])))[0]
    out.append({"t0": round(g["t0"], 3), "t1": round(g["t1"], 3), "strip": s, "cat": cat, "intent": intent,
                "detail": detail, "control": g["control"], "before": g["before"], "after": g["after"], "eq": g.get("eqstate") or None, "steps": g.get("steps", [])})
# mutes of several strips at once (a scene, the end of a set) are one message
merged = []
for x in out:
    y = merged[-1] if merged else None
    if (y and x["cat"] == "mute" == y["cat"] and x["t0"] - y["t0"] < 0.5 and x["strip"] and y.get("strip") is not None
            and x["intent"].split()[0] == y["intent"].split()[0]):
        y.setdefault("group", [y["intent"].split(" ", 1)[1]]).append(x["intent"].split(" ", 1)[1])
        y.setdefault("details", [y["detail"]]).append(x["detail"]); y.setdefault("strips", [y["strip"]]).append(x["strip"])
        y["t1"] = max(y["t1"], x["t1"]); continue
    merged.append(x)
for y in merged:
    if "group" in y:
        verb = y["intent"].split()[0]; names = y["group"]
        y["intent"] = f"{verb} {len(names)} strips: " + ", ".join(n.replace("the ", "") for n in names)
        y["detail"] = ", ".join(d.rsplit(" ", 1)[0] for d in y["details"]) + (" muted" if verb == "Mute" else " on")
out = merged
json.dump(out, open(f"{P}/analysis/{SET}-gestures.json", "w"), ensure_ascii=False, indent=0)
print(len(events), "changes →", len(out), "messages")
