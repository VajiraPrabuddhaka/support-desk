# Shared settings and helpers for the OpenChoreo scripts. Source it, do not run it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NAMESPACE=default
PROJECT=support-desk
ENVIRONMENT=development
# Docker on this machine pushes to the cluster's registry as localhost:10082, and
# the cluster pulls the same images as host.k3d.internal:10082.
REGISTRY_PUSH=localhost:10082
REGISTRY_PULL=host.k3d.internal:10082
# What scripts/push-images.sh pushed, for scripts/deploy-agents.sh.
IMAGES_ENV="$ROOT/openchoreo/.images.env"

[[ -f .env ]] || { echo "no .env in $ROOT: run 'make env' first" >&2; exit 1; }
set -a
# shellcheck disable=SC1091
. ./.env
set +a

log() { printf '\033[1m==> %s\033[0m\n' "$*"; }

# dp_namespace prints the data-plane namespace of the project in the environment.
dp_namespace() {
  kubectl get namespace \
    -l "openchoreo.dev/namespace=$NAMESPACE,openchoreo.dev/project=$PROJECT,openchoreo.dev/environment=$ENVIRONMENT" \
    -o jsonpath='{.items[0].metadata.name}'
}

# openbao_put PATH writes the JSON object on stdin to secret/PATH in OpenBao. The
# values go through stdin, so they never show up in a command line.
openbao_put() {
  kubectl exec -i -n openbao openbao-0 -- sh -c \
    "umask 077; cat > /tmp/kv.json && BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root bao kv put -mount=secret '$1' @/tmp/kv.json >/dev/null; rc=\$?; rm -f /tmp/kv.json; exit \$rc"
}

# json_from_env KEY=VAR... prints a JSON object whose KEY holds the value of VAR.
json_from_env() {
  python3 -I -c '
import json, os, sys
out = {}
for arg in sys.argv[1:]:
    key, var = arg.split("=", 1)
    if not os.environ.get(var):
        sys.exit(f"{var} is not set in .env")
    out[key] = os.environ[var]
print(json.dumps(out))' "$@"
}

# wait_for DESCRIPTION SECONDS COMMAND... runs COMMAND every 3 seconds until it
# succeeds, for at most SECONDS.
wait_for() {
  local what=$1 timeout=$2
  shift 2
  local deadline=$((SECONDS + timeout))
  until "$@" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      echo "timed out after ${timeout}s waiting for $what" >&2
      return 1
    fi
    sleep 3
  done
}

# render copies stdin to stdout, with every ${VAR} replaced by the value of VAR.
# It fails on a variable that is not set.
render() {
  python3 -I -c '
import os, re, sys
text = sys.stdin.read()
def value(m):
    if m.group(1) not in os.environ:
        sys.exit(f"{m.group(1)} is not set")
    return os.environ[m.group(1)]
sys.stdout.write(re.sub(r"\$\{([A-Z0-9_]+)\}", value, text))'
}

# content_hash PATH... prints a short hash of the files under the paths.
content_hash() {
  find "$@" -type f ! -name '.DS_Store' -print0 | LC_ALL=C sort -z | xargs -0 shasum | shasum | cut -c1-12
}
