#!/bin/bash
# A demo of the whole skill, from the repository alone: tabula records a
# made-up show from tabula-emu holding testdata/scenes/demo.scn (the audio
# made up by demo_audio.py, the engineer by demo_desk.py, two songs marked
# with the setlist), then every step runs on it, to a still and a video.
# Used by CI, and for the screenshot of the documentation.
#   make_demo.sh OUT_DIR      (about 90 s of recording, then a few minutes)
# VIDEO=0 stops after the still. EMU_PORT / HTTP_PORT: 18986/18987.
set -euo pipefail
D=$(cd "$(dirname "$(readlink -f "$0")")" && pwd); S=$(dirname "$D")
REPO=$(cd "$S/../../../.." && pwd)
BIN=$(python3 -I "$S/common.py" get TABULA_BIN)
mkdir -p "$1"; OUT=$(cd "$1" && pwd); T="$OUT/tmp"; mkdir -p "$T" "$OUT/sessions"
EP=${EMU_PORT:-18986}; HP=${HTTP_PORT:-18987}; URL=http://127.0.0.1:$HP
E=; B=
trap '[ -n "$B" ] && kill $B 2>/dev/null; [ -n "$E" ] && kill $E 2>/dev/null; true' EXIT

# --- the show, recorded by tabula ---------------------------------------
"$BIN/tabula-emu" -listen 127.0.0.1:$EP -scene "$REPO/testdata/scenes/demo.scn" >"$T/emu.log" 2>&1 & E=$!
sleep 0.5
python3 -I "$D/demo_desk.py" setup 127.0.0.1:$EP
( cd "$T" && exec "$BIN/tabula" -console 127.0.0.1:$EP -http 127.0.0.1:$HP -sessions "$OUT/sessions" -scenes "$T/scenes" \
    -access "$T/access.json" -log none -audio-cmd "python3 -I '$D/demo_audio.py' {channels} {rate}" -audio-channels 32 \
    -audio-tracks 1-3,6,8-10,12-14,16-20,23-24 ) >"$T/tabula.log" 2>&1 & B=$!
for i in $(seq 1 60); do
  ok=$(curl -s -m 2 $URL/api/status | python3 -I -c 'import json,sys; c=json.load(sys.stdin)["console"]; print(c["total"] > 0 and c["synced"] == c["total"])' 2>/dev/null || true)
  [ "$ok" = True ] && break; sleep 0.5
done
[ "$ok" = True ] || { echo "the console never synced" >&2; cat "$T/tabula.log" >&2; exit 1; }
curl -s -f -X PUT -H 'Content-Type: application/json' -d '{"songs": ["First Song - The Demo Band", "Second Song - The Demo Band"]}' $URL/api/setlist >/dev/null
curl -s -f -X POST -H 'Content-Type: application/json' -d '{"note": "demo"}' $URL/api/record/start >/dev/null
echo "recording the demo show (about 90 s)"
python3 -I "$D/demo_desk.py" show 127.0.0.1:$EP $URL
curl -s -f -X POST $URL/api/record/stop >/dev/null
sleep 1; kill $B $E; wait $B $E 2>/dev/null || true; B=; E=
SET=$(basename "$(ls -d "$OUT"/sessions/20*/ | tail -1)")
echo "session $SET"

# --- the video project ----------------------------------------------------
P="$OUT/project"; mkdir -p "$P"; cd "$P"
python3 -I - "$OUT/sessions" <<'EOF'
import json, sys
json.dump({
    "sessions": sys.argv[1], "title": "The Demo Band", "subtitle": "Demo Hall · Sometown · 1 January 2027",
    "metadata_title": "The Demo Band - Demo Hall, Sometown, 1 January 2027", "output": "render/demo.mp4", "lang": "en",
    "foh": {"tracks": [23, 24], "matrix": 1, "label": "FOH"}, "outputs": [{"matrix": 3, "label": "Delay spk"}],
    "fx": [{"fx": 1, "short": "Room", "spoken": "the room reverb", "send_bus": 13, "send_track": 17},
           {"fx": 2, "short": "Hall", "spoken": "the hall reverb", "send_bus": 14, "send_track": 18},
           {"fx": 3, "short": "Delay", "spoken": "the delay", "send_bus": 15, "send_track": 19},
           {"fx": 4, "short": "Chorus", "spoken": "the chorus", "send_bus": 16, "send_track": 20}],
    "delay_fx": 3, "dca_hidden": [5], "voices": [12, 13, 14, 16],
    "onsets": {"instr": {"1": -40, "2": -40, "6": -45, "8": -45, "9": -40, "10": -50}, "gap": 2.5},
    "spoken": {"Hang": "the hang", "Cajon": "the cajon", "Snare": "the snare", "Auto harpe": "the autoharp",
               "Guitare Elec": "the electric guitar", "Guitare Acc": "the acoustic guitar", "Ukulele": "the ukulele"},
}, open("project.json", "w"), ensure_ascii=False, indent=1)
EOF
"$S/dump_state.sh" "$SET"
python3 -I "$S/levels.py" "$SET" >/dev/null
python3 -I "$S/rta.py" "$SET" >/dev/null
python3 -I "$S/setlist.py" markers "$SET"
# the parts and chapters: the set from its first sound, each song from where its music starts
python3 -I - "$S" "$SET" <<'EOF'
import json, sys
sys.path.insert(0, sys.argv[1]); from common import project, meta; from onsets import music_start
cfg = project(); name = sys.argv[2]; m = meta(cfg, name)["audio"]
songs = json.load(open("analysis/setlist.json"))["songs"]
a = m["offset"] + 2.0; b = m["offset"] + m["frames"] / m["rate"] - 1.0
chapters = [{"t": round(music_start(name, s["t"] + 2) - 1 - a, 2), "title": s["title"] + (" - " + s["artist"] if s["artist"] else "")} for s in songs]
# a greeting under 10 s is too short for a chapter of its own: the first song starts at 00:00:00
if chapters[0]["t"] < 10: chapters[0]["t"] = 0.0
else: chapters.insert(0, {"t": 0.0, "title": "GREETINGS"})
json.dump({"sets": [{"name": name, "a": a, "b": b, "fade_in": 0.3, "fade_out": 2.0}], "chapters": chapters},
          open("analysis/segments.json", "w"), indent=1)
EOF
python3 -I "$S/chapters.py" | tee chapters.txt
python3 -I "$S/changes.py" "$SET"
python3 -I "$S/audio.py"
# a still while the lead voice's EQ changes: the curve over its RTA
TV=$(python3 -I -c '
import json, sys
seg = json.load(open("analysis/segments.json")); a = seg["sets"][0]["a"]
g = next(x for x in json.load(open("analysis/" + sys.argv[1] + "-gestures.json")) if x["cat"] == "eq")
print(round(g["t0"] - a + 1.6, 2))' "$SET")
python3 -I "$S/render.py" still still.png "$TV"
echo "still at ${TV} s: $P/still.png"
if [ "${VIDEO:-1}" = 1 ]; then N=2 JOBS=2 "$S/make_video.sh"; fi
