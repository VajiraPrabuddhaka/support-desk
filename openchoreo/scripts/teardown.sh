#!/usr/bin/env bash
# Removes the support desk from OpenChoreo: the console, the agents, the
# components, the resources (and their data), the project and the secret
# references. The platform kinds in openchoreo/platform and the values in
# OpenBao stay.
. "$(dirname "$0")/common.sh"

log "Console"
kubectl delete -f openchoreo/app/desk-console.yaml --ignore-not-found --wait=true

log "Agents and their builds"
# Deleting an Agent deletes its bindings, its releases and the Workload that its
# build wrote.
kubectl delete agent billing-agent support-triage -n "$NAMESPACE" --ignore-not-found --wait=true
kubectl delete workflowruns -n "$NAMESPACE" -l 'openchoreo.dev/agent in (billing-agent,support-triage)' --ignore-not-found

log "Components, resources and the build"
for f in openchoreo/app/{gitea,gitea-mcp,lago-pdf,lago-api,lago-worker,lago-clock,lago-ui,lago-mcp,lago-data}.yaml; do
  kubectl delete -f "$f" --ignore-not-found --wait=true
done
kubectl delete workflowrun lago-mcp-build-01 -n "$NAMESPACE" --ignore-not-found

log "Project and secret references"
kubectl delete -f openchoreo/app/secret-references.yaml --ignore-not-found
kubectl delete -f openchoreo/app/project.yaml --ignore-not-found --wait=true
