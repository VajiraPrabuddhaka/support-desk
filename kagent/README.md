# Run the Support Desk Agents on kagent

This guide walks you through running the two agents of the support desk on [kagent](https://kagent.dev) 1.0.1 and Agent Substrate, on the OpenChoreo cluster of the [main guide](../openchoreo/README.md). In the main guide each agent is a Deployment. Here kagent runs every conversation with an agent in its own sandbox, and the sandbox holds no credentials.

The two agents are written again for kagent in this folder, and a second AgentType (`agent-kagent`) deploys them. The rest of the support desk (Gitea, Lago, the MCP servers and the console) is the one of the main guide. Everything of this variant is in this folder: the agents' code, the OpenChoreo kinds and the values for kagent's installer.

It needs OpenChoreo with the agent kinds (`Agent`, `AgentType`), which are on the `feat/agent-kinds` branch.

## Table of Contents

- [What Is Different](#what-is-different)
- [How the Agents Run on kagent](#how-the-agents-run-on-kagent)
- [How the Agents Are Written](#how-the-agents-are-written)
- [Prerequisites](#prerequisites)
- [Deploy](#deploy)
- [Try It Out](#try-it-out)
- [Folder Layout](#folder-layout)
- [Known Limits](#known-limits)
- [Clean Up](#clean-up)

---

## What Is Different

| | Deployment variant | kagent variant |
|---|---|---|
| Where an agent runs | A Deployment per agent | A sandbox per conversation, on the worker pods of the project's WorkerPool |
| AgentType | `support-desk-agent` | `agent-kagent` |
| ProjectType | `default` | `agents-kagent`, which adds the WorkerPool |
| The agents' code | `/agents` | `/kagent/agents`, a Go module of its own |
| The image | Built by OpenChoreo from GitHub | Built and pushed by you, and pinned by digest in the Agent |
| The system prompt | A file of the Workload | Inline in the Agent |
| The model and its key | Env vars, with the key from a Secret | A Resource of the type `llm-model`, which becomes a kagent ModelConfig. The key stays outside the sandbox |
| The tool allowlist | In the agent's code | In the Agent (`tools.mcp[].tools`), enforced by kagent |
| How a caller reaches an agent | The agent's Service | kagent's gateway, at `/agents/<namespace>/<agent>` |
| What a triage leaves behind | Nothing | A kagent Session with its history, kept for 7 days by default |

The Agents themselves stay close to the ones of the main guide. Compare `openchoreo/agents/support-triage.yaml` here with [the one there](../openchoreo/agents/support-triage.yaml): the same tools, the same skill and the same prompt, on another type.

---

## How the Agents Run on kagent

kagent has no Deployment per agent. `agent-kagent` (in `openchoreo/platform/agent-type.yaml`) renders kagent objects from an Agent, and kagent turns them into what Substrate runs:

| From the Agent | kagent object |
|---|---|
| Image, command and plain env vars | `Harness` (bring your own image) |
| System prompt, skills and MCP tool lists | `Agent` |
| Each MCP server under `tools.mcp` | `RemoteMCPServer` |
| The model Resource under `models.resources` | `ModelConfig` |
| Each agent under `tools.a2a` | `EgressPolicy` |

**What the type adds around them:**

- **A NetworkPolicy per MCP server** - An agent's calls leave from Substrate's egress gateway, not from the project's own pods, and OpenChoreo's policy admits only those. This one admits the gateway on the server's port.
- **A Service named after the agent** - It points to kagent's gateway, which serves the agent's A2A endpoint. OpenChoreo reads an agent's address from a Service, so without it an Agent that names this one under `tools.a2a` would never resolve.

**How a call flows:**

1. The console calls support-triage at kagent's gateway (`kagent-controller.kagent`, port 8083), at `/agents/<namespace>/support-triage`.
2. kagent starts a sandbox for the conversation on a worker pod of the pool `agents`, from a snapshot of the agent's process.
3. The agent's own calls leave through Substrate's egress gateway. It lets through only what the agent's kagent objects name: OpenRouter, gitea-mcp, and billing-agent's address.
4. For a billing ticket, support-triage calls billing-agent at kagent's gateway, which starts a sandbox for it the same way.

**How credentials flow:**

- **Agents → OpenRouter** - The `openrouter` Resource copies the key from OpenBao into a Secret of the project's namespace. The agent sends a placeholder, and Substrate's egress gateway puts the key in its place. The key never enters the sandbox.
- **Agents → Gitea and Lago** - As in the main guide: the agents hold no tokens, and gitea-mcp and lago-mcp act with their own.
- **Console → support-triage → billing-agent** - No credentials yet. kagent accepts such a call in its `insecure` mode only, which is the installer's default.

---

## How the Agents Are Written

The code is in `agents/` and `internal/kit/`, in a Go module of its own (`go.mod` in this folder). It builds on kagent's Go library, which gives a program kagent's runtime contract: A2A on port 80 for kagent's gateway, readiness on port 8081, and every task saved through kagent's task store.

The module is separate for the following reasons:

1. **It follows kagent's release.** kagent supports a runtime only with the library of its own release, so `go.mod` requires the commit of v1.0.1. Rebuild the images when you upgrade kagent.
2. **It keeps the other programs free of that.** The agents in `/agents` and the console keep their own dependencies and their own Go version.

**What an agent gets from kagent, and so from its Agent on OpenChoreo:**

- **The system prompt and the skills** - kagent hands over the prompt and copies each skill to `/skills`. As in `/agents`, every skill is added to the instruction.
- **The model** - From the ModelConfig, with a placeholder for the key.
- **The MCP servers and their tools** - From the RemoteMCPServers. kagent shows the model only the tools that the Agent lists.

**What stays in the program:**

- **support-triage** - The rule that keeps triage inside the support repository (`guardTools`), and the hand-off of billing tickets to billing-agent over A2A, through kagent's gateway.
- **billing-agent** - Today's date in the prompt, read on every request.

One `Dockerfile` builds both images, with `AGENT` naming the agent. Each image has the same command (`/usr/local/bin/agent`), which the Agent's workload names, and the folders that kagent's library writes to (`/config`, `/skills`, `/plugins` and `/data`).

---

## Prerequisites

Before you begin, ensure you have:

- The cluster, the tools and the `.env` of the main guide's [Prerequisites](../openchoreo/README.md#prerequisites). The cluster needs Kubernetes 1.37 or later, as Substrate uses the `PodCertificateRequest` and `ClusterTrustBundle` APIs. `kubectl api-resources --api-group=certificates.k8s.io` lists both.
- Steps 1 and 2 of the main guide done: the secrets are in OpenBao, and the two skill images are in the cluster's registry.
- No `support-desk` project of the type `default`. A project's type cannot change, so if you deployed the main guide's variant, remove it first (see its [Clean Up](../openchoreo/README.md#clean-up)).
- `helm` v3 and `curl` on your machine
- Memory for about **2 GB** more, for kagent and Substrate

---

## Deploy

Run the commands from the root of this repository, in one shell, with `.env` loaded as in the main guide:

```bash
set -a; . ./.env; set +a
```

> [!IMPORTANT]
> Once kagent is installed, the cluster has two kinds named `Agent`. The commands below use the full names, `agents.openchoreo.dev` and `agents.api.kagent.dev`. Do the same in the main guide's commands (e.g., `kubectl get agents.openchoreo.dev -n default`).

### Step 1: Install kagent

Download kagent's CLI, which is also its installer:

```bash
mkdir -p .tools
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSL -o .tools/kagent "https://github.com/kagent-dev/kagent/releases/download/v1.0.1/kagent-${OS}-${ARCH}"
chmod +x .tools/kagent
.tools/kagent version
```

Install kagent into the current kube context. The installer wants an API key for its own default model and does not check it. The agents of the support desk do not use that model, so a placeholder is fine:

```bash
export OPENAI_API_KEY=placeholder
.tools/kagent --config kagent/openchoreo/install/kagent-config.yaml install
```

The install takes 5 to 10 minutes, most of it pulling images. If it stops with `context deadline exceeded`, run the same command again. It continues where it stopped.

**What gets installed:**

- **kagent** (namespace `kagent`) - The controller, which is also the A2A gateway, its database, a worker pool of its own and the UI.
- **Agent Substrate** (namespace `ate-system`) - What runs the sandboxes: its API, the node agent (atelet), the egress gateway and an object store for snapshots.
- **A certificate controller** (namespace `podcertificate-controller-system`) - Gives the pods above their identities.

**What the values in `openchoreo/install/` add to the installer's defaults:**

- **`substrate-values.yaml`** - Lets the agents of the support desk use the Secrets of their own namespace. Substrate's egress gateway reads the model's key only where such a grant allows it.
- **`atelet-values.yaml`** - Substrate pulls images itself, over HTTPS unless the registry is localhost. The cluster's registry serves plain HTTP, so `agent-kagent` names it `localhost:10082` for Substrate, and this file maps localhost to the registry's address in the cluster.
- **`kagent-values.yaml`** - One image that the chart leaves unset.

Check the result:

```bash
helm list -A | grep -E 'kagent|substrate'
kubectl get pods -n kagent
kubectl get pods -n ate-system
```

You should see six `deployed` releases, and every pod `Running` (one bucket job `Completed`).

### Step 2: Apply the Platform Kinds

```bash
kubectl apply -f openchoreo/platform/
kubectl apply -f kagent/openchoreo/platform/
```

The first command is Step 3 of the main guide. The second adds the kinds of this variant:

- **`agent-kagent` (AgentType)** - Runs an agent on kagent. See [How the Agents Run on kagent](#how-the-agents-run-on-kagent).
- **`agents-kagent` (ProjectType)** - The shipped `default` type, plus a Substrate WorkerPool named `agents` in the project's namespace. kagent looks for an agent's worker pool in the agent's own namespace, so each project needs one.
- **`llm-model` (ResourceType)** - A model that a provider serves over the OpenAI API, with its key from the secret store.
- **A ClusterRole for OpenChoreo's cluster agent** - The cluster agent applies a release to the data plane, and its own ClusterRole lists kinds one by one. This one adds kagent's kinds and the WorkerPool. It is the one cluster-wide object of this variant, besides kagent itself.

### Step 3: Create the Project

Create the project with the type `agents-kagent`, in place of Step 4 of the main guide:

```bash
kubectl apply -f kagent/openchoreo/app/project.yaml
kubectl apply -f openchoreo/app/secret-references.yaml
```

OpenChoreo creates the project's namespace in the data plane, with the WorkerPool in it. Keep the namespace's name, and wait for the worker pod:

```bash
NS=$(kubectl get namespace \
  -l openchoreo.dev/namespace=default,openchoreo.dev/project=support-desk,openchoreo.dev/environment=development \
  -o jsonpath='{.items[0].metadata.name}')
echo "$NS"
kubectl get workerpools -n "$NS"
kubectl wait --for=condition=Ready pod -l ate.dev/worker-pool=agents -n "$NS" --timeout=5m
```

> [!NOTE]
> The namespace is `dp-default-support-desk-development-d9be60f2` on every cluster, as OpenChoreo derives it from the names `default`, `support-desk` and `development`. `install/substrate-values.yaml` and `app/desk-console-binding.yaml` name it. If you rename the project, change it in both.

### Step 4: Deploy the Rest of the Support Desk

The project is new, so it has none of the support desk yet. Follow the following steps of the main guide, and skip its Step 7, which deploys the agents as Deployments:

- **[Step 5](../openchoreo/README.md#step-5-deploy-lago-gitea-and-the-mcp-servers)** - Lago, Gitea and the two MCP servers
- **[Step 6](../openchoreo/README.md#step-6-seed-gitea-and-lago)** - The seed, which also gives gitea-mcp its token
- **[Step 8](../openchoreo/README.md#step-8-deploy-and-build-the-console)** - The console

> [!NOTE]
> If you deployed the support desk on this cluster before, restart `gitea-mcp` after the seed, as the note at the end of Step 6 says. OpenBao still has the old Gitea tokens, and `gitea-mcp` starts with one of them.

The agents wait for their tools, so make sure both MCP servers are ready before you go on:

```bash
kubectl wait --for=condition=Ready releasebinding/gitea-mcp-development \
  releasebinding/lago-mcp-development -n default --timeout=10m
```

### Step 5: Build and Push the Agents' Images

kagent wants an image pinned by digest, and OpenChoreo's build writes a tag. So build the two images and push them to the cluster's registry yourself, as you did for the skills:

```bash
for agent in billing-agent support-triage; do
  image=localhost:10082/support-desk/kagent/$agent:v1
  docker build --build-arg AGENT=$agent -t "$image" kagent/
  docker push "$image"
done
```

The first build takes a few minutes, as it downloads kagent's Go modules. Then read the digests that the registry gave the images:

```bash
digest() {
  docker inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "localhost:10082/support-desk/kagent/$1:v1" |
    grep '^localhost:10082/' | sed 's/.*@sha256://' | head -1
}
BILLING_AGENT_DIGEST=$(digest billing-agent)
SUPPORT_TRIAGE_DIGEST=$(digest support-triage)
echo "$BILLING_AGENT_DIGEST $SUPPORT_TRIAGE_DIGEST"
```

### Step 6: Deploy the Model and the Agents

Apply the model, which is a Resource. As in the main guide, a Resource has no autoDeploy, so point its binding at the Resource's release. If the Resource has no release yet, wait a few seconds and run the last two commands again:

```bash
kubectl apply -f kagent/openchoreo/app/openrouter.yaml
release=$(kubectl get resource openrouter -n default -o jsonpath='{.status.latestRelease.name}')
kubectl patch resourcereleasebinding openrouter-development -n default --type=merge \
  -p "{\"spec\":{\"resourceRelease\":\"$release\"}}"
```

Apply the two Agents, each with the digest of its image. billing-agent goes first, as support-triage names it under `tools.a2a`:

```bash
sed "s/BILLING_AGENT_DIGEST/$BILLING_AGENT_DIGEST/" kagent/openchoreo/agents/billing-agent.yaml | kubectl apply -f -
sed "s/SUPPORT_TRIAGE_DIGEST/$SUPPORT_TRIAGE_DIGEST/" kagent/openchoreo/agents/support-triage.yaml | kubectl apply -f -
```

Each Agent pins its skill, cuts a release and binds it to `development`. kagent then prepares the agent: Substrate pulls the image, starts the program once and keeps a snapshot of it, which every conversation starts from. Wait for both:

```bash
kubectl wait --for=condition=Ready agentreleasebinding/billing-agent-development \
  agentreleasebinding/support-triage-development -n default --timeout=10m
```

> [!TIP]
> A binding that stays not ready says why in its conditions (`kubectl describe agentreleasebinding billing-agent-development -n default`). With `A dependency has no address yet`, an MCP server of the agent is not deployed, or billing-agent is not ready yet in support-triage's case. `status.pendingConnections` names it.

Look at what `agent-kagent` rendered, and at what kagent says about it:

```bash
kubectl get agents.api.kagent.dev,harnesses,modelconfigs,remotemcpservers,egresspolicies -n "$NS"
kubectl get agents.api.kagent.dev -n "$NS" \
  -o jsonpath='{range .items[*]}{.metadata.name}{": "}{range .status.conditions[*]}{.type}={.status}{" "}{end}{"\n"}{end}'
```

To ship a change to an agent, push its image again and apply the Agent with the new digest. To change the model, edit `model` in `app/openrouter.yaml` and apply the file again.

### Step 7: Point the Console at kagent

The console sets support-triage's address by hand, as a Component cannot depend on an Agent yet. Apply its binding with the agent's address on kagent's gateway:

```bash
kubectl apply -f kagent/openchoreo/app/desk-console-binding.yaml
kubectl wait --for=condition=Ready releasebinding/desk-console-development -n default --timeout=5m
```

The address is the URL of the agent card, `http://kagent-controller.kagent.svc.cluster.local:8083/agents/<namespace>/support-triage/.well-known/agent-card.json`. kagent's gateway serves many agents, so the console needs the whole URL and not only the host.

> [!NOTE]
> The console reads the steps of a triage from labels on what the agent streams back, and kagent uses labels of its own. The console knows both since this variant was added, so build it from a revision that has this folder.

That's it! The support desk now triages with the agents on kagent.

---

## Try It Out

Open the console at http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080, pick a ticket and click **Triage with AI**, as in the main guide. kagent starts a sandbox for every triage, so allow a little longer than with the Deployments.

You can also triage from the terminal, with the `ask` client through a port-forward to kagent's gateway:

```bash
kubectl port-forward -n kagent svc/kagent-controller 8083:8083 >/dev/null &
go run ./tools/ask -agent "http://localhost:8083/agents/$NS/support-triage/.well-known/agent-card.json" "Triage ticket #1."
```

To ask billing-agent directly, use `billing-agent` in the URL.

In a second terminal, follow what the agents do. Their output is in the log of the worker pods (set `NS` there as in Step 3):

```bash
kubectl logs -f -n "$NS" -l ate.dev/worker-pool=agents --prefix --tail 20
```

Every triage is a kagent Session, so kagent's own UI shows it with its history:

```bash
kubectl port-forward -n kagent svc/kagent-ui 8082:8080 >/dev/null &
```

Then open http://localhost:8082. The Sessions belong to `admin@kagent.dev`, the user that kagent gives a caller without credentials.

Check the state of the agents on both sides:

```bash
kubectl get agents.openchoreo.dev,agentreleasebindings,resourcereleasebindings -n default
kubectl get agents.api.kagent.dev,workerpools,pods -n "$NS"
```

---

## Folder Layout

```
kagent/
├── agents/              the two agents as kagent runtimes
├── internal/kit/        what both share: loading what kagent compiled, and serving
├── Dockerfile           one image per agent (--build-arg AGENT=<name>)
├── go.mod               a Go module of its own, on kagent's library
└── openchoreo/
    ├── platform/        the AgentType, ProjectType and ResourceType, and the cluster agent's ClusterRole
    ├── app/             the project, the model Resource and the console's binding
    ├── agents/          the two Agents, each with its binding in development
    └── install/         the values for kagent's installer
```

---

## Known Limits

- **Agent builds** - OpenChoreo does not build these agents. kagent wants a digest and a command, and the build writes a tag and no command. So the workload is inline in the Agent, and you push the image yourself.
- **System prompt** - It is inline in the Agent, as there is no build to copy it from a file. Keep it in line with `/agents/<name>/prompt.md` by hand.
- **Agent to agent** - support-triage calls billing-agent without credentials, which kagent accepts in its `insecure` mode only. kagent 1.0.1 has no binding from one agent to another, so the call is the image's own, and billing-agent's Session belongs to kagent's default user.
- **Agent addresses** - An agent on kagent has no Service of its own pods. The Service that `agent-kagent` renders points to kagent's gateway, and the type adds `/agents/<namespace>/<agent>` for a caller under `tools.a2a`. So both agents have to be on this type, and there is no Endpoint for them.
- **Network** - For agents on kagent the network no longer tells projects apart: the MCP servers admit Substrate's egress gateway, which carries the calls of every agent. Substrate's per-agent egress rules do that instead.
- **OpenChoreo's view of an agent** - Replicas, resources and pod logs do not apply, as there are no pods per agent. The logs are in the worker pods, and kagent's UI shows the conversations.
- **Sessions** - Every triage starts a new kagent Session. Idle ones are suspended and kept for 7 days by default (`sessions.idleTtl` of kagent's controller).
- **Registry** - The mapping of localhost to the cluster's registry is for a local cluster. A registry with TLS needs neither that nor the `registry` settings of `agent-kagent`.
- **Secrets grant** - Substrate needs a grant per project namespace before it reads a model's key there, and the grant is in Substrate's values. So a platform engineer adds it for each project.
- **kagent upgrades** - Rebuild the agents' images with kagent's library of the new release. kagent does not support a runtime of another release.

The limits of the main guide about the seed, the console and OpenRouter's free models apply here as well.

---

## Clean Up

Delete the agents and the model first, then the rest of the support desk as in the main guide:

```bash
kubectl delete -f kagent/openchoreo/agents/
kubectl delete -f kagent/openchoreo/app/openrouter.yaml
kubectl delete -f openchoreo/build/ --ignore-not-found
kubectl delete -f openchoreo/app/
```

The last command deletes the project as well, as it has the same name here. The platform kinds stay. Remove them with `kubectl delete -f kagent/openchoreo/platform/ -f openchoreo/platform/` if you no longer need them.

kagent's installer created six Helm releases. To remove kagent, uninstall them in the reverse order of the install:

```bash
helm uninstall kagent -n kagent
helm uninstall substrate -n ate-system
helm uninstall substrate-atelet -n ate-system
helm uninstall substrate-podcert -n podcertificate-controller-system
helm uninstall substrate-crds -n ate-system
helm uninstall kagent-crds -n kagent
```
