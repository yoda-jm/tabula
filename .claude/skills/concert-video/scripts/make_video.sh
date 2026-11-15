#!/bin/bash
# The whole video, from the project folder: N pieces rendered JOBS at a time
# (render.py piped to x264), joined without re-encoding, with
# audio/concert-master.wav and the chapters.
#   OUT     the video (default: project.json "output")
#   PIECES  the pieces' folder (default render/pieces): give a new one, with
#           a new OUT, to render a version next to one being watched
#   MUX_ONLY=1  keep the pieces, only join them again (sound or chapters changed)
#   N=8 JOBS=2  pieces and parallel renders (each render takes about one core
#           and x264 another)
set -euo pipefail
D=$(cd "$(dirname "$(readlink -f "$0")")" && pwd)
N=${N:-8}; JOBS=${JOBS:-2}
OUT=${OUT:-$(python3 -I -c 'import json; print(json.load(open("project.json"))["output"])')}
PIECES=${PIECES:-render/pieces}
FRAMES=$(python3 -I -c 'import json; s=json.load(open("analysis/segments.json"))["sets"]; print(int(sum(x["b"]-x["a"] for x in s)*30))')
PER=$(( (FRAMES + N - 1) / N ))
mkdir -p "$PIECES" "$(dirname "$OUT")"; PIECES=$(cd "$PIECES" && pwd)
piece() {
  i=$1; first=$(( i * PER )); count=$(( PER < FRAMES - first ? PER : FRAMES - first ))
  python3 -I "$D/render.py" frames $first $count | ffmpeg -nostdin -loglevel error -y -f rawvideo -pix_fmt bgr0 -s 2560x1072 -r 30 -i - \
    -c:v libx264 -preset veryfast -tune animation -crf 18 -pix_fmt yuv420p -g 60 "$PIECES/p$i.mp4"
  echo "piece $i done $(date +%T)"
}
export -f piece; export PER FRAMES PIECES D
[ "${MUX_ONLY:-}" = 1 ] || seq 0 $((N - 1)) | xargs -P $JOBS -I{} bash -c 'piece {}'
for i in $(seq 0 $((N - 1))); do echo "file '$PIECES/p$i.mp4'"; done > "$PIECES/list.txt"
python3 -I "$D/chapters.py" ffmeta > "$PIECES/chapters.ffmeta"
ffmpeg -nostdin -loglevel error -y -f concat -safe 0 -i "$PIECES/list.txt" -i audio/concert-master.wav -i "$PIECES/chapters.ffmeta" \
  -map 0:v -map 1:a -map_metadata 2 -map_chapters 2 -c:v copy -c:a aac -b:a 384k -shortest -movflags +faststart "$OUT"
echo "video done $(date +%T): $OUT"
