# Deploy the Support Desk on OpenChoreo

This guide walks you through deploying the support desk on a local OpenChoreo cluster, by applying the YAML files in this folder with `kubectl`. The two agents run as OpenChoreo **Agents**, each with an **Endpoint** for its A2A endpoint, Gitea, Lago and the support desk console run as **Components**, and Lago's database and Redis are **Resources**. OpenChoreo builds the two agents and the console from this repository on GitHub. The OpenChoreo artifacts are kept in this folder, so the whole deployment lives next to the code.

It needs OpenChoreo with the agent kinds (`Agent`, `AgentType`), their builds, the `A2A` endpoint protocol and Endpoints that route to an Agent, which are on the `feat/agent-kinds` branch.

Here each agent runs as a Deployment. To run the two agents on kagent, with every conversation in its own sandbox, see [kagent/README.md](../kagent/README.md).

## Table of Contents

- [What Gets Deployed](#what-gets-deployed)
- [How the Agents Are Put Together](#how-the-agents-are-put-together)
- [How the Agents Are Built](#how-the-agents-are-built)
- [Prerequisites](#prerequisites)
- [Deploy](#deploy)
- [Try It Out](#try-it-out)
- [Folder Layout](#folder-layout)
- [Known Limits](#known-limits)
- [Clean Up](#clean-up)

---

## What Gets Deployed

Everything belongs to one project, `support-desk`, in the `development` environment.

| Piece | OpenChoreo kind | Type |
|---|---|---|
| support-triage | Agent | `support-desk-agent` (ours), built by OpenChoreo's `dockerfile-builder` |
| billing-agent | Agent | `support-desk-agent` (ours), built by OpenChoreo's `dockerfile-builder` |
| support-triage-a2a, billing-agent-a2a | Endpoint | `a2a-http` (ours), one for the A2A endpoint of each agent |
| gitea | Component | `web-application`, with two `persistent-volume-claim` traits |
| gitea-mcp | Component | `service`, internal only |
| lago-db | Resource | `postgres-partman` (ours) |
| lago-redis | Resource | `valkey` |
| lago-api | Component | `web-application`, with a `persistent-volume-claim` trait |
| lago-worker | Component | `worker-with-shared-volume` (ours) |
| lago-clock | Component | `worker` |
| lago-pdf | Component | `service`, internal only |
| lago-ui | Component | `web-application` |
| lago-mcp | Component | `service`, internal only, built by OpenChoreo's `dockerfile-builder` |
| desk-console | Component | `web-application`, built by OpenChoreo's `dockerfile-builder` |

The four types marked "ours" are in `platform/`. They are namespaced in `default`, so nothing cluster-wide changes:

- **`support-desk-agent` (AgentType)** - Runs an agent as a Deployment that serves A2A. See the next section.
- **`a2a-http` (EndpointType)** - Exposes the A2A endpoint of an agent, with A2A over HTTP. `support-desk-agent` allows it for the protocol `A2A`. See [How the A2A Endpoints Are Exposed](#how-the-a2a-endpoints-are-exposed).
- **`postgres-partman` (ResourceType)** - The shipped `postgres` type with Lago's image, which has the `pg_partman` extension that Lago's migrations install. The shipped type runs plain `postgres:16-alpine`.
- **`worker-with-shared-volume` (ComponentType)** - The shipped `worker` type plus one volume owned by another component. `lago-worker` writes invoice PDFs to the volume that `lago-api` serves them from. The shipped types allow only the traits they list, so a type of our own avoids changing them.

---

## How the Agents Are Put Together

Each Agent splits its instructions in two, and the agent code reads both from files that OpenChoreo mounts:

| Part | Where it comes from | Mounted at |
|---|---|---|
| **System prompt** (role and steps) | `agents/<name>/prompt.md`, which the build copies into the Workload as the file `prompt.md`, and which `spec.systemPrompt.valueFrom.workloadFile` names | `/var/run/openchoreo/prompt/system-prompt.md` (`AGENT_SYSTEM_PROMPT_FILE`) |
| **Skills** (rules: the triage rubric, the billing policy) | `agents/<name>/skills/<skill>/SKILL.md`, as an image that the Agent controller pins by digest | `/skills/<skill>` (`AGENT_SKILLS_DIR`) |

The prompt comes in the way a component's config file does. The file stays in the source, the workload descriptor (`agents/<name>/workload.yaml`) names it with `valueFrom.path`, and the build copies its content into the Workload (`files[].value`). The Agent then says which file is its system prompt:

```yaml
spec:
  systemPrompt:
    valueFrom:
      workloadFile: prompt.md      # or, for inline text: value: |
```

The release freezes the file's text as the agent's system prompt, and the AgentType mounts it for the agent. Every agent type reads it the same way, as `agent.systemPrompt`, and nobody creates a ConfigMap by hand.

The release freezes both, so every release records exactly what its agent ran with. Without these files, for example in Docker Compose, the agents use the copies built into their binaries.

The AgentType also renders the rest of what an agent needs:

- **Tool addresses** - The addresses of `gitea-mcp`, `lago-mcp` and `billing-agent`, in the env vars that each tool's `envBindings` names (`GITEA_MCP_URL`, `LAGO_MCP_URL`, `BILLING_AGENT_URL`).
- **A2A** - `LISTEN_ADDR` and `PUBLIC_URL` for the endpoint named `a2a`, which each agent declares with the protocol `A2A` in its `workload.yaml`. `PUBLIC_URL` is the agent's in-cluster URL, so the agent card points to the same origin that callers resolve.
- **Env, files and secrets** - The workload's own env vars and files, with secrets from OpenBao through SecretReferences.

**How credentials flow:**

- **support-triage → gitea-mcp** - The agent sends no credentials. gitea-mcp holds the triage bot's Gitea token, a file mounted from the `support-desk-triage-bot` secret (`GITEA_ACCESS_TOKEN_FILE`), and acts as the triage bot in Gitea.
- **billing-agent → lago-mcp** - The agent sends no credentials. lago-mcp holds the Lago API key from the `support-desk-lago` secret (`LAGO_API_KEY`) and calls Lago with it.
- **support-triage → billing-agent** - Plain A2A inside the cluster, with no authentication yet.
- **desk-console → Gitea and Lago** - The support-inbox bot's Gitea token is a file mounted from the `support-desk-support-inbox` secret, and the Lago API key comes from `support-desk-lago`. The console only reads from Lago.

### How the A2A Endpoints Are Exposed

Each agent declares its A2A endpoint in `workload.yaml`, with the name `a2a` and the protocol `A2A`. OpenChoreo exposes it through an Endpoint, the way it exposes a component's endpoint:

- **`allowedEndpoints`** - `support-desk-agent` allows the EndpointType `a2a-http` for the protocol `A2A`, as the default. A client that creates an agent's Endpoints (the portal, `occ`) picks it, and the files in `agents/` name it themselves.
- **The Endpoint** - Each file in `agents/` has an Endpoint for the agent's `a2a` endpoint (`support-triage-a2a`, `billing-agent-a2a`). It is owned by the agent (`spec.owner.agentName`), so deleting the agent deletes it, and it deploys to `development` with `autoDeploy`.
- **`a2a-http`** - Routes to the Service that `support-desk-agent` renders for the agent. It accepts A2A endpoints only, and serves `project` and `external` visibility. A2A runs over HTTP here, so an agent that serves A2A over gRPC would need a type of its own.

The Endpoints ask for `project` visibility only, as both callers are in the project. Each one reports the agent's in-cluster URL on its EndpointReleaseBinding (`status.urls.project`), but the callers do not read it yet. support-triage gets billing-agent's address from `tools.a2a`, which resolves it from the agent's Service, and the console sets support-triage's address by hand. That is also why the Service stays in the AgentType for now. See [Known Limits](#known-limits).

---

## How the Agents Are Built

OpenChoreo builds each agent from this repository on GitHub. The Agent names its build in `spec.workflow`, and `support-desk-agent` allows `dockerfile-builder` in its `allowedWorkflows`:

```yaml
spec:
  workflow:
    kind: ClusterWorkflow
    name: dockerfile-builder
    parameters:
      repository:
        url: https://github.com/VajiraPrabuddhaka/support-desk
        revision:
          branch: main
        appPath: /agents/support-triage
      docker:
        context: /
        filePath: /agents/support-triage/Dockerfile
```

A build is a WorkflowRun of that workflow, with the `openchoreo.dev/project` and `openchoreo.dev/agent` labels. You apply it from `build/`. It runs the following steps:

1. It checks out the repository at the latest commit of the branch.
2. It builds `agents/<name>/Dockerfile`, with the root of the repository as the context, and pushes the image to the cluster's registry as `default-support-desk-<name>:v1-<commit>`.
3. It reads `agents/<name>/workload.yaml` and writes the Workload `<name>-workload` with the image it pushed. The Workload names the Agent as its owner (`owner.agentName`), and the descriptor's `prompt.md` (`valueFrom.path`) goes inline into it.

The Agent takes its workload from that Workload. Each new image cuts a new release, which autoDeploy binds to `development`. Until the first build writes the Workload, the Agent is not ready, with the reason `AwaitingBuild`.

**Key notes:**

- **Entrypoint** - A workload descriptor has no command, so each agent's Dockerfile builds only that agent and sets it as the `ENTRYPOINT`.
- **Model** - The model is a choice of the environment, not of the build. Each Agent's binding in `development` sets `OPENROUTER_MODEL` with `workloadOverrides`, in `agents/<name>.yaml`, so changing the model needs no new build.
- **Credentials** - The descriptors reference secrets by name (`secretKeyRef`), as the values stay in OpenBao.

The console is built the same way, as a Component. `desk-console` names `dockerfile-builder` in its `spec.workflow`, its build runs `console/Dockerfile`, and the build writes the Workload `desk-console-workload` from `console/workload.yaml`. Its release binding adds the public links to Gitea and Lago with `workloadOverrides`, as these depend on the environment.

---

## Prerequisites

Before you begin, ensure you have:

- OpenChoreo running on a local k3d or kind cluster, built from `feat/agent-kinds` and installed with the workflow plane (`--with-build` in the k3d installer). A build writes an Agent's Workload with `occ workload create --agent`, so the cluster needs the `openchoreo-cli` image from that branch as well. The steps use the current kube context, so switch to the cluster first (e.g., `kubectl config use-context kind-openchoreo`).
- The local setup that the k3d installer gives you, which a kind cluster has to match: the cluster reaches your machine as `host.k3d.internal`, the registry is at `localhost:10082`, and the data plane gateway is at `openchoreoapis.localhost:19080`.
- The code on GitHub. The Agents, the console and their builds use the `main` branch of https://github.com/VajiraPrabuddhaka/support-desk. To build your own changes, push them to your fork and change `repository.url` in `agents/*.yaml`, `app/desk-console.yaml` and `build/*-build.yaml`.
- The Agent controller allowed to pin skills from the cluster's registry over HTTP:
  ```bash
  kubectl -n openchoreo-control-plane set env deployment/controller-manager \
    AGENT_SKILL_INSECURE_REGISTRIES=host.k3d.internal:10082
  ```
- Docker, Go 1.26 or later, `jq` and `kubectl` on your machine
- A `.env` with your OpenRouter key: `./scripts/init-env.sh` creates it with generated secrets, and then you set `OPENROUTER_API_KEY` (see the main [README](../README.md))
- Memory for about 4 GB of extra pods. If the Docker Compose version of the support desk runs on the same Docker VM, stop it first with `docker compose down`.

---

## Deploy

Run the commands from the root of this repository, in one shell. Some of them read values from `.env`, so load it first:

```bash
set -a; . ./.env; set +a
```

### Step 1: Store the Secrets in OpenBao

The SecretReferences in `app/secret-references.yaml` read the support desk's secrets from OpenBao, under `secret/support-desk/`. Write the OpenRouter key and Lago's keys from `.env`. `jq` builds the JSON and `bao` reads it from stdin, so the values never show up in a command line:

```bash
jq -n '{apiKey: env.OPENROUTER_API_KEY}' |
  kubectl exec -i -n openbao openbao-0 -- env BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root \
    bao kv put -mount=secret support-desk/openrouter -

jq -n 'env | {
    secretKeyBase: .LAGO_SECRET_KEY_BASE,
    rsaPrivateKey: .LAGO_RSA_PRIVATE_KEY,
    encryptionPrimaryKey: .LAGO_ENCRYPTION_PRIMARY_KEY,
    encryptionDeterministicKey: .LAGO_ENCRYPTION_DETERMINISTIC_KEY,
    encryptionKeyDerivationSalt: .LAGO_ENCRYPTION_KEY_DERIVATION_SALT,
    adminPassword: .LAGO_ADMIN_PASSWORD,
    apiKey: .LAGO_API_KEY}' |
  kubectl exec -i -n openbao openbao-0 -- env BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root \
    bao kv put -mount=secret support-desk/lago -
```

The two bots' Gitea tokens follow in Step 6, once Gitea is seeded.

### Step 2: Push the Skill Images

OpenChoreo builds the agents and the console, in Steps 7 and 8. An Agent takes a skill only from an OCI image, though, so build the two skill images and push them to the cluster's registry, which your machine reaches as `localhost:10082` and the cluster as `host.k3d.internal:10082`:

```bash
for skill in agents/support-triage/skills/triage-rubric agents/billing-agent/skills/billing-policy; do
  image=localhost:10082/support-desk/skills/$(basename "$skill"):v1
  printf 'FROM scratch\nCOPY . /\n' | docker build -t "$image" -f - "$skill"
  docker push "$image"
done
```

A skill image holds the skill's files at its root, as the Agent controller and the image volume that mounts it expect. The tags match the image references in `agents/*.yaml`. To ship a change to a skill, push it under a new tag and update the reference, so that a new release picks it up.

### Step 3: Apply the Platform Kinds

```bash
kubectl apply -f openchoreo/platform/
```

This adds the `support-desk-agent` AgentType, the `a2a-http` EndpointType, the `postgres-partman` ResourceType and the `worker-with-shared-volume` ComponentType.

### Step 4: Create the Project

```bash
kubectl apply -f openchoreo/app/project.yaml
kubectl apply -f openchoreo/app/secret-references.yaml
```

OpenChoreo creates a data plane namespace for the project in `development`. The later steps reach Gitea, Lago and the agents there, so keep its name (it can take a few seconds to show up):

```bash
NS=$(kubectl get namespace \
  -l openchoreo.dev/namespace=default,openchoreo.dev/project=support-desk,openchoreo.dev/environment=development \
  -o jsonpath='{.items[0].metadata.name}')
echo "$NS"
```

### Step 5: Deploy Lago, Gitea and the MCP Servers

Start with Lago's database and Redis, which are Resources:

```bash
kubectl apply -f openchoreo/app/lago-data.yaml
```

A Resource has no autoDeploy, so point each binding at its Resource's release. If a Resource has no release yet, wait a few seconds and run this again:

```bash
for r in lago-db lago-redis; do
  release=$(kubectl get resource "$r" -n default -o jsonpath='{.status.latestRelease.name}')
  if [ -n "$release" ]; then
    kubectl patch resourcereleasebinding "$r-development" -n default --type=merge \
      -p "{\"spec\":{\"resourceRelease\":\"$release\"}}"
  else
    echo "$r has no release yet"
  fi
done
```

Then apply the components:

```bash
kubectl apply -f openchoreo/app/lago-pdf.yaml -f openchoreo/app/lago-api.yaml \
  -f openchoreo/app/lago-worker.yaml -f openchoreo/app/lago-clock.yaml -f openchoreo/app/lago-ui.yaml
kubectl apply -f openchoreo/app/gitea.yaml -f openchoreo/app/gitea-mcp.yaml -f openchoreo/app/lago-mcp.yaml
```

> [!NOTE]
> Each component file applies its ReleaseBinding and Workload before the Component. That way, autoDeploy finds the environment settings (resources, volume sizes, external URLs) in place and only fills in the release.

Start the build of `lago-mcp`:

```bash
kubectl apply -f openchoreo/build/lago-mcp-build.yaml
```

The build takes a few minutes, as it compiles Lago's MCP server from source. Until it finishes, `lago-mcp` runs the published image as a stand-in. The build writes its image into the Workload named `lago-mcp-workload`, which is why that Workload has this name: the build finds it and replaces only its image, and keeps the endpoint, the dependency and the command. Applying `app/lago-mcp.yaml` again puts the stand-in back, so start a new build if you do.

Wait until Gitea and the Lago API are up. The first start takes a few minutes, as it pulls the images and runs Lago's migrations:

```bash
kubectl wait --for=condition=Ready releasebinding/gitea-development releasebinding/lago-api-development \
  -n default --timeout=15m
kubectl rollout status deployment -n "$NS" -l openchoreo.dev/component=lago-api --timeout=15m
kubectl rollout status deployment -n "$NS" -l openchoreo.dev/component=gitea --timeout=10m
```

On a fresh install, `gitea-mcp` stays in `ContainerCreating` until Step 6 stores the triage bot's token, as its pod mounts that token.

### Step 6: Seed Gitea and Lago

OpenChoreo has no one-off job type, so the seed runs from your machine. First, create the Gitea admin account inside the Gitea pod. The values reach the pod on stdin:

```bash
GITEA_POD=$(kubectl get pod -n "$NS" -l openchoreo.dev/component=gitea -o name | head -1)
{ echo "export GITEA_ADMIN_USER='$GITEA_ADMIN_USER' GITEA_ADMIN_PASSWORD='$GITEA_ADMIN_PASSWORD' GITEA_ADMIN_EMAIL='$GITEA_ADMIN_EMAIL'"
  cat infra/gitea/create-admin.sh; } | kubectl exec -i -n "$NS" "$GITEA_POD" -- sh -s
```

Then run the seed program against Gitea and the Lago API through port-forwards. It creates the five scenarios, and writes the two bots' Gitea tokens to `.secrets/openchoreo/`:

```bash
kubectl port-forward -n "$NS" svc/gitea 13001:3000 >/dev/null & GITEA_PF=$!
kubectl port-forward -n "$NS" svc/lago-api 13000:3000 >/dev/null & LAGO_PF=$!
sleep 3
GITEA_URL=http://localhost:13001 LAGO_API_URL=http://localhost:13000 SECRETS_DIR=.secrets/openchoreo \
  go run ./tools/seed
kill $GITEA_PF $LAGO_PF
```

Finally, write the two tokens to OpenBao, where the secret references read them: triage-bot's for gitea-mcp, and support-inbox's for the console:

```bash
for bot in triage-bot support-inbox; do
  jq -n --rawfile t ".secrets/openchoreo/$bot.token" '{token: ($t | rtrimstr("\n"))}' |
    kubectl exec -i -n openbao openbao-0 -- env BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root \
      bao kv put -mount=secret "support-desk/$bot" -
done
```

These two secret references refresh every minute, so `gitea-mcp` starts shortly after.

> [!NOTE]
> On a second deployment, OpenBao still has the tokens of the first one. `gitea-mcp` then starts right away, with a token that the new Gitea does not know, and every call to Gitea fails with `401 invalid username, password or token`. It reads the token once, from a file that does not follow the Secret. So restart it after you write the new tokens:
>
> ```bash
> kubectl rollout restart deployment -n "$NS" -l openchoreo.dev/component=gitea-mcp
> ```

### Step 7: Deploy and Build the Agents

Apply the two Agents. Each file applies the Agent's binding in `development` first, with the model, then the Agent, and then the Endpoint for the agent's A2A endpoint:

```bash
kubectl apply -f openchoreo/agents/billing-agent.yaml -f openchoreo/agents/support-triage.yaml
```

The Agents are not ready yet, with the reason `AwaitingBuild`, as no build has written their Workloads. Start the builds:

```bash
kubectl apply -f openchoreo/build/billing-agent-build.yaml -f openchoreo/build/support-triage-build.yaml
```

Each build checks out `main` from GitHub, builds the agent's Dockerfile, pushes the image and writes the Agent's Workload (see [How the Agents Are Built](#how-the-agents-are-built)). The first build takes a few minutes, as it downloads the Go modules. Follow the builds, and wait for the agents once both builds succeed:

```bash
kubectl get workflowruns -n default -l openchoreo.dev/agent -w
kubectl wait --for=condition=Ready agent/billing-agent agent/support-triage -n default --timeout=10m
kubectl wait --for=condition=Ready agentreleasebinding/billing-agent-development \
  agentreleasebinding/support-triage-development -n default --timeout=10m
```

Once a build writes the Workload, the Agent controller pins the skills, cuts a release and binds it to `development`.

The agents' Endpoints wait for the same Workloads, as an Endpoint checks the agent's `a2a` endpoint in it. Once the agents are deployed, each Endpoint deploys to `development` and reports the agent's in-cluster URL:

```bash
kubectl get endpoints.openchoreo.dev,endpointreleasebindings -n default
kubectl get endpointreleasebinding billing-agent-a2a-development -n default -o jsonpath='{.status.urls.project}'
```

To deploy a change to an agent, push it to GitHub and apply a copy of its build file under a new name (for example, `support-triage-build-02`), as a WorkflowRun runs once. To change an agent's model, edit `OPENROUTER_MODEL` in its binding in `agents/<name>.yaml` and apply the file again. That needs no new build.

### Step 8: Deploy and Build the Console

Apply the console's release binding and Component, and start its build:

```bash
kubectl apply -f openchoreo/app/desk-console.yaml
kubectl apply -f openchoreo/build/desk-console-build.yaml
```

The build checks out `main` from GitHub, builds `console/Dockerfile`, pushes the image and writes the Workload `desk-console-workload` from `console/workload.yaml`. The Component then cuts a release, and autoDeploy binds it to `development`. Follow the build, and wait for the console once the build succeeds:

```bash
kubectl get workflowrun desk-console-build-01 -n default -w
kubectl wait --for=condition=Ready releasebinding/desk-console-development -n default --timeout=10m
```

The console is the `desk-console` Component, a `web-application`. It reaches Gitea and the Lago API through endpoint dependencies, like any other component. support-triage is the exception: a Component cannot depend on an Agent yet, so the Workload sets `TRIAGE_AGENT_URL` to `http://support-triage:8001`, the agent's Service in the project's namespace. The agent's network policy lets in every pod of the project, so the call goes through.

The console's name is kept short on purpose. OpenChoreo shortens a longer component name in the external hostname, so `support-console`, for example, would show up as `support-conso`.

---

## Try It Out

Open the console at http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080. Pick a ticket and click **Triage with AI**. The agent activity panel shows each step as the agents work, and the account panel shows the customer's invoices and coupons in Lago. **New request** files a new ticket the way a customer would. See [Step 3 of the main README](../README.md#step-3-triage-tickets-in-the-console) for the parts of the console.

You can also triage from the terminal, with the `ask` client through a port-forward to support-triage's Service:

```bash
kubectl port-forward -n "$NS" svc/support-triage 18000:8001 >/dev/null &
go run ./tools/ask -agent http://localhost:18000 "Triage ticket #1."
```

Ask for "Triage all untriaged tickets." to triage every untriaged ticket. To ask billing-agent directly, forward `svc/billing-agent` on port 8002 the same way.

In a second terminal, follow what the agents do (set `NS` there as in Step 4):

```bash
kubectl logs -f -n "$NS" -l 'openchoreo.dev/agent in (support-triage,billing-agent)' \
  --prefix --max-log-requests 4 --tail 20
```

Check the state of the support desk:

```bash
kubectl get agents,agentreleasebindings,workflowruns -n default
kubectl get endpoints.openchoreo.dev,endpointreleasebindings -n default
kubectl get releasebindings,resourcereleasebindings -n default
kubectl get pods -n "$NS"
```

Open the UIs at the following URLs:

| UI | URL | Sign in with |
|---|---|---|
| Console | http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080 | No sign-in |
| Gitea | http://http-gitea-development-default-e9dbaee7.openchoreoapis.localhost:19080 | `GITEA_ADMIN_USER` and `GITEA_ADMIN_PASSWORD` from `.env` |
| Lago | http://http-lago-ui-development-default-01c488b9.openchoreoapis.localhost:19080 | `LAGO_ADMIN_EMAIL` and `LAGO_ADMIN_PASSWORD` from `.env` |

The hostnames follow OpenChoreo's naming, `<endpoint>-<component>-<environment>-<namespace>-<hash>`, where the hash is computed from the same names. They do not change between deployments, so the release bindings in `app/` set them as Gitea's and Lago's external URLs, and as the console's links to both.

---

## Folder Layout

```
openchoreo/
├── platform/        the AgentType, EndpointType, ResourceType and ComponentType of the support desk
├── app/             the project, secret references, resources and components, the console included
├── agents/          the two Agents, each with its binding in development and its A2A Endpoint
└── build/           the WorkflowRuns that build lago-mcp, the two agents and the console
```

Each agent's `Dockerfile` and `workload.yaml` are next to its code in `agents/<name>/` at the root of the repository, and the console's are in `console/`, where the builds read them.

---

## Known Limits

- **Agent builds** - A build starts from a WorkflowRun that you apply. Agents do not build on a push yet (`autoBuild`).
- **Skills** - An Agent takes a skill only from an OCI image, so you build and push the skills' images yourself. Skills from Git are not supported yet.
- **Seed** - It runs from your machine, as there is no one-off job type.
- **Environments** - Only `development`. An Agent deploys to the first environment of its project's pipeline.
- **A2A** - The call between the agents is not authenticated yet.
- **A2A Endpoints** - The callers do not use the URL that an agent's Endpoint reports yet, as an agent's `tools.a2a` resolves only from the Service that its callee's AgentType renders. That is why `support-desk-agent` still renders the Service, and `a2a-http` routes to it instead of rendering its own.
- **External A2A** - `a2a-http` can expose an agent on the external gateway (`external` visibility), but an A2A client that follows the agent card cannot call the agent through it yet. The card carries the agent's in-cluster `PUBLIC_URL`, so the client is sent to an address it cannot reach. `ask` and the console are not affected, as they point the card at the URL they were given. The Endpoints ask for `project` visibility only.
- **Agent credentials** - The agents carry none for now. gitea-mcp and lago-mcp act with their own credentials for any caller in the project, and lago-mcp offers every Lago tool, as the tool allowlist lives in billing-agent.
- **Console to agent** - A Component cannot declare a dependency on an Agent yet, so the console's Workload sets support-triage's address by hand.
- **Console sign-in** - The console has no sign-in. Anyone who can open its URL can file and triage tickets, which is fine for a local cluster only.
- **Billing limits** - They live in the billing policy skill. Lago does not enforce them.
- **OpenRouter free models** - A free model allows 50 requests a day on an account without credits, and triaging one billing ticket takes about 15 across both agents. After the limit, calls fail with `429 Too Many Requests` until the next day (UTC). Credits raise the limit, or let you use a paid model.

---

## Clean Up

Delete the agents first, then the builds, then the rest of the support desk:

```bash
kubectl delete -f openchoreo/agents/
kubectl delete -f openchoreo/build/ --ignore-not-found
kubectl delete -f openchoreo/app/
```

Deleting an Agent also deletes its releases, its Endpoint and the Workload that its build wrote. `kubectl` goes through the files in `app/` in alphabetical order, so the project and the secret references go last. The platform kinds in `platform/` and the values in OpenBao stay. Remove them with `kubectl delete -f openchoreo/platform/` and from OpenBao if you no longer need them.
