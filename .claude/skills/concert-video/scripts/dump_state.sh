#!/bin/bash
# The console at the start of each set as tabula sees it, for the drawing and
# the chat: a throwaway tabula-emu loads the session's start.scn, a throwaway
# tabula reads it, and its state and schema are saved in analysis/
# (<set>-state0.json, schema.json). Nothing else is touched: own ports, own
# temporary folder (sessions, scenes, access), and only these two processes
# are stopped.
#   dump_state.sh SET...    (from the project folder)
# The tabula binaries: TABULA_BIN (default the repository's bin/, `make build`).
# EMU_PORT / HTTP_PORT: 18976/18977.
set -euo pipefail
D=$(cd "$(dirname "$(readlink -f "$0")")" && pwd)
BIN=$(python3 -I "$D/common.py" get TABULA_BIN)
EP=${EMU_PORT:-18976}; HP=${HTTP_PORT:-18977}
SESS=$(python3 -I -c 'import sys; sys.path.insert(0, sys.argv[1]); from common import project; print(project()["sessions"])' "$D")
mkdir -p analysis
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
for s in "$@"; do
  "$BIN/tabula-emu" -listen 127.0.0.1:$EP -scene "$SESS/$s/start.scn" >"$T/emu.log" 2>&1 & E=$!
  ( cd "$T" && exec "$BIN/tabula" -http 127.0.0.1:$HP -console 127.0.0.1:$EP -sessions "$T/sessions" \
      -scenes "$T/scenes" -access "$T/access.json" -log none >"$T/tabula.log" 2>&1 ) & B=$!
  ok=
  for i in $(seq 1 60); do
    ok=$(curl -s -m 2 http://127.0.0.1:$HP/api/status | python3 -I -c 'import json,sys; c=json.load(sys.stdin)["console"]; print(c["total"] > 0 and c["synced"] == c["total"])' 2>/dev/null || true)
    [ "$ok" = True ] && break; sleep 0.5
  done
  if [ "$ok" = True ]; then
    curl -s 'http://127.0.0.1:'$HP'/api/state?prefix=/' > "analysis/$s-state0.json"
    curl -s http://127.0.0.1:$HP/api/schema > analysis/schema.json
    echo "$s: $(python3 -I -c "import json; print(len(json.load(open('analysis/$s-state0.json'))))" ) nodes"
  else
    echo "$s: the console never synced; see $T/tabula.log" >&2; cat "$T/tabula.log" >&2
  fi
  kill $B $E; wait $B $E 2>/dev/null || true
done
