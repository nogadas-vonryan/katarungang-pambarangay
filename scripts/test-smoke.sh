#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$ROOT_DIR/api"

bru run \
  system/health.yml system/status.yml auth/login.yml stores/get_all.yml cases/get_all.yml files/get_all.yml backups/get_all.yml jobs/get_all.yml auth/logout.yml \
  --env-file environments/test.json \
  --bail --noproxy
