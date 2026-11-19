#!/usr/bin/env bash
# Deploys everything except the agents: the project, the secret references, Lago's
# database and Redis, the Gitea and Lago components and the two MCP servers, and
# starts the build of lago-mcp.
. "$(dirname "$0")/common.sh"
APP=openchoreo/app

log "Project $PROJECT"
kubectl apply -f "$APP/project.yaml"
wait_for "the project's data plane namespace" 180 dp_namespace
log "Data plane namespace: $(dp_namespace)"

log "Secret references"
kubectl apply -f "$APP/secret-references.yaml"

# A Resource has no autoDeploy: point each binding at the Resource's latest release.
log "Resources lago-db and lago-redis"
kubectl apply -f "$APP/lago-data.yaml"
for resource in lago-db lago-redis; do
  wait_for "a release of $resource" 120 \
    test -n "$(kubectl get resource "$resource" -n "$NAMESPACE" -o jsonpath='{.status.latestRelease.name}')"
  release=$(kubectl get resource "$resource" -n "$NAMESPACE" -o jsonpath='{.status.latestRelease.name}')
  kubectl patch resourcereleasebinding "$resource-$ENVIRONMENT" -n "$NAMESPACE" --type=merge \
    -p "{\"spec\":{\"resourceRelease\":\"$release\"}}"
done

log "Components"
for c in lago-pdf lago-api lago-worker lago-clock lago-ui gitea gitea-mcp; do
  kubectl apply -f "$APP/$c.yaml"
done
# Keep the image that a build already wrote (see the note in lago-mcp.yaml).
LAGO_MCP_IMAGE=$(kubectl get workload lago-mcp-workload -n "$NAMESPACE" -o jsonpath='{.spec.container.image}' 2>/dev/null || true)
export LAGO_MCP_IMAGE=${LAGO_MCP_IMAGE:-getlago/lago-mcp-server:latest}
render < "$APP/lago-mcp.yaml" | kubectl apply -f -

log "Build of lago-mcp"
if kubectl get workflowrun lago-mcp-build-01 -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "workflowrun/lago-mcp-build-01 exists; to rebuild, apply a copy of openchoreo/build/lago-mcp-build.yaml with a new name"
else
  kubectl apply -f openchoreo/build/lago-mcp-build.yaml
fi

log "Waiting for Lago and Gitea (the first start pulls images and runs Lago's migrations)"
for b in lago-db-development lago-redis-development; do
  kubectl wait --for=condition=Ready "resourcereleasebinding/$b" -n "$NAMESPACE" --timeout=10m
done
for b in gitea lago-api lago-pdf lago-ui; do
  kubectl wait --for=condition=Ready "releasebinding/$b-$ENVIRONMENT" -n "$NAMESPACE" --timeout=15m
done
ns=$(dp_namespace)
kubectl rollout status deployment -n "$ns" -l openchoreo.dev/component=lago-api --timeout=15m
kubectl rollout status deployment -n "$ns" -l openchoreo.dev/component=gitea --timeout=10m
log "Done. Next: make oc-bootstrap"
