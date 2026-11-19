#!/usr/bin/env bash
# Deploys the support desk console, a web-application Component. Its image,
# ${CONSOLE_IMAGE}, comes from what scripts/push-images.sh pushed.
. "$(dirname "$0")/common.sh"

[[ -f "$IMAGES_ENV" ]] || { echo "no $IMAGES_ENV: run 'make oc-images' first" >&2; exit 1; }
set -a
# shellcheck disable=SC1090
. "$IMAGES_ENV"
set +a

log "Component desk-console"
render < openchoreo/app/desk-console.yaml | kubectl apply -f -

log "Waiting for the console"
kubectl wait --for=condition=Ready "releasebinding/desk-console-$ENVIRONMENT" -n "$NAMESPACE" --timeout=5m
kubectl rollout status deployment -n "$(dp_namespace)" -l openchoreo.dev/component=desk-console --timeout=5m
log "Done. Open http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080"
