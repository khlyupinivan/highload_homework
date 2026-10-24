#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/app"
POD_NAME=pod-1 DC=dc-msk DEPLOYMENT=crud-api go run ./cmd/server &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
sleep 2
curl -s localhost:8080/__meta/routes | head -c 400; echo
curl -s localhost:8080/users -o /dev/null
curl -s localhost:8080/metrics | grep -E 'http_(requests|request_latency)' | head