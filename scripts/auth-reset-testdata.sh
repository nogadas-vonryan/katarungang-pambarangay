#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

rm -f "$ROOT_DIR/data-test/.system/auth.db"
