#!/usr/bin/env bash
# Deploys the two agents, which OpenChoreo builds from this repository on GitHub:
#   1. applies each Agent, and its binding in the development environment,
#   2. starts a WorkflowRun of each Agent's workflow at the head commit of
#      SOURCE_BRANCH in SOURCE_REPO. The build pushes the image and writes the
#      Agent's Workload from agents/<name>/workload.yaml,
#   3. waits until the Agents have cut a release from that Workload, and
#      autoDeploy has deployed it.
# The build checks out what is on GitHub, not this working copy, so push your
# changes first. The skill images come from what scripts/push-images.sh pushed,
# and the model from OPENROUTER_MODEL in .env.
. "$(dirname "$0")/common.sh"

[[ -f "$IMAGES_ENV" ]] || { echo "no $IMAGES_ENV: run 'make oc-images' first" >&2; exit 1; }
set -a
# shellcheck disable=SC1090
. "$IMAGES_ENV"
set +a
: "${OPENROUTER_MODEL:=anthropic/claude-haiku-5.5}"
: "${SOURCE_REPO:=https://github.com/VajiraPrabuddhaka/support-desk}"
: "${SOURCE_BRANCH:=main}"
export OPENROUTER_MODEL SOURCE_REPO SOURCE_BRANCH
agents=(billing-agent support-triage)

for agent in "${agents[@]}"; do
  log "Agent $agent"
  render < "openchoreo/agents/$agent.yaml" | kubectl apply -f -
done

commit=$(git ls-remote "$SOURCE_REPO" "refs/heads/$SOURCE_BRANCH" | cut -f1)
[[ -n "$commit" ]] || { echo "no branch $SOURCE_BRANCH in $SOURCE_REPO" >&2; exit 1; }
log "Builds of $SOURCE_REPO, branch $SOURCE_BRANCH, at ${commit:0:8}"
if [[ "$(git rev-parse HEAD 2>/dev/null)" != "$commit" ]] || [[ -n "$(git status --porcelain -- agents internal go.mod go.sum)" ]]; then
  echo "note: this working copy differs from that commit; the builds use the commit"
fi

# One run per agent and commit, so running this again does not build twice. The
# run uses the Agent's own workflow, pinned to the commit.
runs=()
for agent in "${agents[@]}"; do
  run="$agent-build-${commit:0:8}"
  runs+=("$run")
  if kubectl get workflowrun "$run" -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "workflowrun/$run exists"
    continue
  fi
  kubectl get agent "$agent" -n "$NAMESPACE" -o json |
    jq --arg run "$run" --arg commit "$commit" '{
      apiVersion: "openchoreo.dev/v1alpha1",
      kind: "WorkflowRun",
      metadata: {
        name: $run,
        namespace: .metadata.namespace,
        labels: {
          "openchoreo.dev/project": .spec.owner.projectName,
          "openchoreo.dev/agent": .metadata.name
        }
      },
      spec: {workflow: (.spec.workflow | .parameters.repository.revision.commit = $commit)}
    }' | kubectl apply -f -
done

log "Waiting for the builds (a first build downloads the Go modules, so it takes a few minutes)"
for run in "${runs[@]}"; do
  kubectl wait --for=condition=WorkflowCompleted "workflowrun/$run" -n "$NAMESPACE" --timeout=30m
  succeeded=$(kubectl get workflowrun "$run" -n "$NAMESPACE" \
    -o jsonpath='{.status.conditions[?(@.type=="WorkflowSucceeded")].status}')
  if [[ "$succeeded" != True ]]; then
    echo "workflowrun/$run failed. See: kubectl get workflowrun $run -n $NAMESPACE -o yaml" >&2
    echo "To build again, delete the run and run this script again." >&2
    exit 1
  fi
  echo "workflowrun/$run succeeded"
done

log "Waiting for the agents"
for agent in "${agents[@]}"; do
  kubectl wait --for=condition=Ready "agent/$agent" -n "$NAMESPACE" --timeout=5m
  kubectl wait --for=condition=Ready "agentreleasebinding/$agent-$ENVIRONMENT" -n "$NAMESPACE" --timeout=10m
done
log "Done. Try: make oc-triage T=1"
