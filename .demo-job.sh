#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")"

echo "== [1/5] dsh-cli status =="
./dsh-cli status || echo "!! status failed"

echo
echo "== [2/5] dsh-cli ls (sessions) =="
./dsh-cli ls || echo "!! ls failed"

echo
echo "== [3/5] dsh-cli workspaces =="
./dsh-cli workspaces || echo "!! workspaces failed"

echo
echo "== [4/5] dsh-cli new (create session) =="
SID=$(./dsh-cli new --cwd "$PWD")
echo "new session: $SID"

echo
echo "== [5/5] one-shot pipe mode (-v, verbose) =="
timeout 180 ./dsh-cli --session "$SID" -v "List the top-level directories of this repo in one short line. No other words." --timeout 170s
rc=$?
echo
echo "== one-shot exit: $rc =="
echo
echo "== done =="
