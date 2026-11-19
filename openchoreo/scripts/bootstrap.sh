#!/usr/bin/env bash
# Prepares Gitea and Lago for the scenarios, from this machine:
#   1. creates the Gitea admin account inside the Gitea pod,
#   2. runs the seed program against Gitea and Lago through port-forwards,
#   3. writes the two bots' Gitea tokens to OpenBao, where the SecretReferences
#      read them: triage-bot's for gitea-mcp, support-inbox's for the console.
# OpenChoreo has no one-off job type, so this runs here. Running it again creates
# nothing twice.
. "$(dirname "$0")/common.sh"
ns=$(dp_namespace)

log "Gitea admin account"
pod=$(kubectl get pod -n "$ns" -l openchoreo.dev/component=gitea -o jsonpath='{.items[0].metadata.name}')
{
  printf 'export GITEA_ADMIN_USER=%q GITEA_ADMIN_PASSWORD=%q GITEA_ADMIN_EMAIL=%q\n' \
    "$GITEA_ADMIN_USER" "$GITEA_ADMIN_PASSWORD" "$GITEA_ADMIN_EMAIL"
  cat infra/gitea/create-admin.sh
} | kubectl exec -i -n "$ns" "$pod" -- sh -s

log "Port-forwards to Gitea and the Lago API"
kubectl port-forward -n "$ns" svc/gitea 13001:3000 >/dev/null 2>&1 &
pf_gitea=$!
kubectl port-forward -n "$ns" svc/lago-api 13000:3000 >/dev/null 2>&1 &
pf_lago=$!
trap 'kill $pf_gitea $pf_lago 2>/dev/null || true' EXIT
wait_for "Gitea on localhost:13001" 60 curl -fsS http://localhost:13001/api/healthz
wait_for "Lago on localhost:13000" 60 curl -fsS http://localhost:13000/health

log "Seed"
secrets_dir=.secrets/openchoreo
GITEA_URL=http://localhost:13001 LAGO_API_URL=http://localhost:13000 SECRETS_DIR="$secrets_dir" \
  go run ./tools/seed

for bot in triage-bot support-inbox; do
  log "OpenBao: secret/support-desk/$bot"
  BOT_TOKEN=$(tr -d '\n' < "$secrets_dir/$bot.token")
  export BOT_TOKEN
  json_from_env token=BOT_TOKEN | openbao_put "support-desk/$bot"
done
unset BOT_TOKEN
log "Done. Next: make oc-agents"
