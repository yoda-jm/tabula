# The show's songs, from whatever there is, into analysis/setlist.json:
#   {"source": "...", "songs": [{"title", "artist", "tempo", "set", "t"}]}
# their order and names, and their times when markers give them.
#   setlist.py markers SET…        the markers set during the show (⚑, M, the
#                                   setlist's "3. Song"), at session seconds;
#                                   tabula's own markers (console, audio gaps) left out
#   setlist.py session SET         the setlist the session was recorded with
#                                   (its setlist.json: songs and tempos)
#   setlist.py text FILE           one song a line: "Title - Artist", a chapter
#                                   list ("HH:MM:SS : Title - Artist") or titles alone
#   setlist.py troubastack BAND CONCERT
#                                   a concert's songs from TROUBASTACK_URL; BAND and
#                                   CONCERT by name (or the concert's date, YYYY-MM-DD).
#                                   The login comes from TROUBASTACK_USER and
#                                   TROUBASTACK_PASSWORD given to this one command;
#                                   it is never written anywhere.
# Markers are pressed a little early or late: onsets.py finds where each song
# really starts. Without times, Whisper (transcribe.py) places the songs.
import sys, os, json, re, http.cookiejar, urllib.request
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import project, session_dir, setting

NUMBER = re.compile(r"^\d+\.\s+")
TIME = re.compile(r"^\s*(\d{1,2}:)?\d{1,2}:\d{2}\s*[:\-–]?\s*")

def auto_marker(label):  # the markers tabula sets by itself (pkg/session AutoMarker)
    return label in ("console connected", "console lost", "console back") or label.startswith(("audio lost", "audio back"))

def song(text, **more):
    title, artist = (text.rsplit(" - ", 1) + [""])[:2] if " - " in text else (text, "")
    return {"title": title.strip(), "artist": artist.strip(), **more}

cfg = project(); how = sys.argv[1]; songs = []
if how == "markers":
    for name in sys.argv[2:]:
        for line in open(os.path.join(session_dir(cfg, name), "events.jsonl")):
            e = json.loads(line)
            label = e.get("marker", "")
            if label and not auto_marker(label):
                songs.append(song(NUMBER.sub("", label), set=name, t=e["t"]))
elif how == "session":
    name = sys.argv[2]
    sl = json.load(open(os.path.join(session_dir(cfg, name), "setlist.json")))
    tempos = sl.get("tempos") or []
    for i, s in enumerate(sl.get("songs", [])):
        songs.append(song(s, set=name, tempo=tempos[i] if i < len(tempos) and tempos[i] else None))
elif how == "text":
    for line in open(sys.argv[2], encoding="utf-8"):
        line = line.strip()
        if not line or (":" not in line and " - " not in line and line.lower() in ("chapters", "chapitrage", "setlist", "tracklist")):
            continue
        songs.append(song(TIME.sub("", line)))
elif how == "troubastack":
    url = setting("TROUBASTACK_URL"); band, concert = sys.argv[2], sys.argv[3]
    user, pw = os.environ.get("TROUBASTACK_USER"), os.environ.get("TROUBASTACK_PASSWORD")
    if not url or not user or not pw:
        sys.exit("TROUBASTACK_URL (.env) and TROUBASTACK_USER / TROUBASTACK_PASSWORD (for this command) are needed")
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def call(path, body=None):
        req = urllib.request.Request(url.rstrip("/") + path, data=json.dumps(body).encode() if body else None,
                                     headers={"Content-Type": "application/json"} if body else {})
        return json.load(op.open(req))
    call("/api/auth/login", {"username": user, "password": pw})
    bands = call("/api/bands"); bands = bands.get("bands", bands) if isinstance(bands, dict) else bands
    b = next((x for x in bands if x["name"].lower() == band.lower() or x["id"] == band), None)
    if not b: sys.exit(f"no band {band!r}: " + ", ".join(x["name"] for x in bands))
    cs = call(f"/api/bands/{b['id']}/concerts"); cs = cs.get("concerts", cs) if isinstance(cs, dict) else cs
    c = next((x for x in cs if concert.lower() in (x.get("name", "").lower(), x.get("eventDate", ""))), None)
    if not c: sys.exit(f"no concert {concert!r}: " + ", ".join(f"{x.get('name')} ({x.get('eventDate')})" for x in cs))
    part = 1
    for s in c.get("songs", []):
        if not s.get("title"): part += 1; continue  # an entry with no song: the break between sets
        songs.append(song(s["title"], set=part))
else:
    sys.exit("setlist.py markers SET… | session SET | text FILE | troubastack BAND CONCERT")
out = os.path.join(cfg["dir"], "analysis", "setlist.json"); os.makedirs(os.path.dirname(out), exist_ok=True)
json.dump({"source": " ".join(sys.argv[1:3]), "songs": songs}, open(out, "w"), ensure_ascii=False, indent=1)
for i, s in enumerate(songs, 1):
    t = f"  {s['t']:8.1f}" if s.get("t") is not None else ""
    print(f"{i:2d}. {s['title']}" + (f" - {s['artist']}" if s["artist"] else "") + t)
