#!/usr/bin/env bash
# Sends one message to an agent on OpenChoreo, through a port-forward to its
# Service. The agents are not exposed outside the cluster.
#   ask.sh <agent> <message>     e.g. ask.sh support-triage "Triage ticket #1."
. "$(dirname "$0")/common.sh"
agent=${1:?usage: ask.sh <agent> <message>}
shift
ns=$(dp_namespace)
port=$(kubectl get svc "$agent" -n "$ns" -o jsonpath='{.spec.ports[0].port}')

kubectl port-forward -n "$ns" "svc/$agent" 18000:"$port" >/dev/null 2>&1 &
pf=$!
trap 'kill $pf 2>/dev/null || true' EXIT
wait_for "a port-forward to $agent" 30 curl -fsS http://localhost:18000/healthz

go run ./tools/ask -agent http://localhost:18000 "$@"
