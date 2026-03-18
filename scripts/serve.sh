#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$ROOT_DIR/scripts/lib/env.sh"

exec go run ./server/cmd/server --data-dir ./data --port 8080
