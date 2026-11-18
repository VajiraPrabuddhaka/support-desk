#!/bin/sh
# Creates the Gitea admin account that the seed program signs in with.
# Runs once in the gitea-init service, on the same volumes as gitea, and does
# nothing if the account exists.
set -eu

: "${GITEA_ADMIN_USER:?}" "${GITEA_ADMIN_PASSWORD:?}" "${GITEA_ADMIN_EMAIL:?}"

if gitea admin user list --admin | awk 'NR > 1 { print $2 }' | grep -qx "$GITEA_ADMIN_USER"; then
  echo "admin account $GITEA_ADMIN_USER already exists"
  exit 0
fi

gitea admin user create \
  --admin \
  --username "$GITEA_ADMIN_USER" \
  --password "$GITEA_ADMIN_PASSWORD" \
  --email "$GITEA_ADMIN_EMAIL" \
  --must-change-password=false
echo "created admin account $GITEA_ADMIN_USER"
