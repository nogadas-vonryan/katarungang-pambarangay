#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$ROOT_DIR/api"

bru run \
  system/health.yml system/status.yml system/setup.yml auth/login.yml stores/reload.yml stores/create.yml cases/create.yml cases/get_one.yml cases/put.yml cases/patch.yml files/upload.yml files/get_all.yml files/get_one.yml files/rename.yml files/delete.yml backups/create.yml backups/get_all.yml backups/restore.yml jobs/get_all.yml jobs/cancel.yml cases/delete.yml auth/logout.yml \
  --env-file environments/test.json \
  --bail --noproxy
