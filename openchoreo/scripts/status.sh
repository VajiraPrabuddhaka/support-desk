#!/usr/bin/env bash
# Shows the state of the support desk on OpenChoreo: bindings, pods and URLs.
. "$(dirname "$0")/common.sh"

log "Agents"
kubectl get agents,agentreleasebindings -n "$NAMESPACE" | grep -E "NAME|billing-agent|support-triage" || true
log "Components and resources"
kubectl get releasebindings,resourcereleasebindings -n "$NAMESPACE" \
  -o custom-columns='KIND:.kind,NAME:.metadata.name,READY:.status.conditions[?(@.type=="Ready")].status,REASON:.status.conditions[?(@.type=="Ready")].reason' |
  grep -E "KIND|gitea|lago|desk-console" || true
log "Builds (the agents and lago-mcp)"
kubectl get workflowruns -n "$NAMESPACE" -l "openchoreo.dev/project=$PROJECT" 2>/dev/null || true

if ns=$(dp_namespace 2>/dev/null) && [[ -n "$ns" ]]; then
  log "Pods in $ns"
  kubectl get pods -n "$ns"
fi

log "URLs"
cat <<EOF
Console:  http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080
Gitea:    http://http-gitea-development-default-e9dbaee7.openchoreoapis.localhost:19080
Lago UI:  http://http-lago-ui-development-default-01c488b9.openchoreoapis.localhost:19080
Lago API: http://http-lago-api-development-default-417fab49.openchoreoapis.localhost:19080
EOF
