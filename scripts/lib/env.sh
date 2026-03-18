#!/bin/sh

if [ -z "${ROOT_DIR:-}" ]; then
  printf '%s\n' "ROOT_DIR is not set before sourcing scripts/lib/env.sh" >&2
  exit 1
fi

ENV_FILE="$ROOT_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
  printf '%s\n' "Missing .env file at $ENV_FILE" >&2
  exit 1
fi

set -a
. "$ENV_FILE"
set +a

if [ -z "${KP_BOOTSTRAP_USER:-}" ]; then
  printf '%s\n' "KP_BOOTSTRAP_USER is required in $ENV_FILE" >&2
  exit 1
fi

if [ -z "${KP_BOOTSTRAP_PASSWORD:-}" ]; then
  printf '%s\n' "KP_BOOTSTRAP_PASSWORD is required in $ENV_FILE" >&2
  exit 1
fi
