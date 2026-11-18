#!/usr/bin/env bash
# Creates .env from .env.example and fills in the generated secrets.
# An existing .env is left alone.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  echo ".env already exists, leaving it as is"
  exit 0
fi
command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

cp .env.example .env
chmod 600 .env

# set_var KEY VALUE replaces the line "KEY=" in .env.
set_var() {
  awk -v k="$1" -v v="$2" 'index($0, k "=") == 1 && length($0) == length(k) + 1 { print k "=" v; next } { print }' \
    .env > .env.tmp && mv .env.tmp .env
}

uuid() {
  local h
  h=$(openssl rand -hex 16)
  echo "${h:0:8}-${h:8:4}-${h:12:4}-${h:16:4}-${h:20:12}"
}

set_var GITEA_ADMIN_PASSWORD "$(openssl rand -hex 16)"
set_var LAGO_ADMIN_PASSWORD "$(openssl rand -hex 16)"
set_var LAGO_API_KEY "$(uuid)"
set_var LAGO_POSTGRES_PASSWORD "$(openssl rand -hex 16)"
set_var LAGO_SECRET_KEY_BASE "$(openssl rand -hex 64)"
set_var LAGO_ENCRYPTION_PRIMARY_KEY "$(openssl rand -hex 16)"
set_var LAGO_ENCRYPTION_DETERMINISTIC_KEY "$(openssl rand -hex 16)"
set_var LAGO_ENCRYPTION_KEY_DERIVATION_SALT "$(openssl rand -hex 16)"
set_var LAGO_RSA_PRIVATE_KEY "$(openssl genrsa 2048 2>/dev/null | openssl base64 -A)"

# Take the OpenRouter key from the shell if it is set there.
if [[ -n "${OPENROUTER_API_KEY:-}" ]]; then
  set_var OPENROUTER_API_KEY "$OPENROUTER_API_KEY"
fi

echo "created .env with generated secrets"
if ! grep -q '^OPENROUTER_API_KEY=.' .env; then
  echo "now set OPENROUTER_API_KEY in .env"
fi
