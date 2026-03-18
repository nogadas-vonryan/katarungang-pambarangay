#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$ROOT_DIR/scripts/lib/env.sh"

PORT=18080
HEALTH_URL="http://localhost:${PORT}/health"
LOG_FILE="/tmp/kp-api-all.log"

if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
  printf '%s\n' "Port ${PORT} is already in use. Stop that server and rerun npm test."
  exit 1
fi

"$ROOT_DIR/scripts/auth-reset-testdata.sh"

go run ./server/cmd/server --data-dir ./data-test --port "$PORT" >"$LOG_FILE" 2>&1 &
pid=$!

cleanup() {
  kill "$pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

i=0
until curl -fsS "$HEALTH_URL" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    printf '%s\n' "Server failed to become ready. See $LOG_FILE"
    exit 1
  fi
  sleep 1
done

cd "$ROOT_DIR/api"

bru run \
  system/health.yml system/status.yml auth/login.yml stores/get_all.yml cases/get_all.yml files/get_all.yml backups/get_all.yml jobs/get_all.yml auth/logout.yml \
  --env-file environments/test.json \
  --env-var base_url="http://localhost:${PORT}" \
  --env-var auth_username="$KP_BOOTSTRAP_USER" \
  --env-var auth_password="$KP_BOOTSTRAP_PASSWORD" \
  --bail --noproxy

bru run \
  system/health.yml system/status.yml system/setup.yml auth/login.yml stores/reload.yml stores/create.yml cases/create.yml cases/get_one.yml cases/put.yml cases/patch.yml files/upload.yml files/get_all.yml files/get_one.yml files/rename.yml files/delete.yml backups/create.yml backups/get_all.yml backups/restore.yml jobs/get_all.yml jobs/cancel.yml cases/delete.yml auth/logout.yml \
  --env-file environments/test.json \
  --env-var base_url="http://localhost:${PORT}" \
  --env-var auth_username="$KP_BOOTSTRAP_USER" \
  --env-var auth_password="$KP_BOOTSTRAP_PASSWORD" \
  --bail --noproxy
