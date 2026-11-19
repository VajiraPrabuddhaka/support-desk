#!/usr/bin/env bash
# Follows the logs of both agents on OpenChoreo, tool calls included.
. "$(dirname "$0")/common.sh"
kubectl logs -f -n "$(dp_namespace)" -l 'openchoreo.dev/agent in (support-triage,billing-agent)' \
  --prefix --max-log-requests 4 --tail 20
