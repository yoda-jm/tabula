#!/bin/sh
# Development loop: tabula-emu with a real scene, tabula (the API, connected
# to the emulator) and the Vite dev server with hot reload, which forwards
# /api to tabula. Open http://localhost:5173. Ctrl-C stops everything.
#
#   TABULA_HTTP=127.0.0.1:8081 SCENE=my.scn ./scripts/dev.sh
set -eu
cd "$(dirname "$0")/.."
HTTP=${TABULA_HTTP:-127.0.0.1:8080}
SCENE=${SCENE:-testdata/scenes/GVO-2026.scn}

go build -o bin/ ./cmd/...
[ -d web/node_modules ] || (cd web && npm ci)

./bin/tabula-emu -listen 127.0.0.1:10023 -scene "$SCENE" -http 127.0.0.1:8032 &
emu=$!
sleep 0.5
./bin/tabula -http "$HTTP" -console 127.0.0.1 &
tab=$!
trap 'kill $emu $tab 2>/dev/null || true' EXIT INT TERM

echo "emulator debug page: http://127.0.0.1:8032  API: http://$HTTP  GUI: http://localhost:5173"
cd web && TABULA_API="http://$HTTP" npm run dev
