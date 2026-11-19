# Deploy the Support Desk on OpenChoreo

This guide walks you through deploying the support desk on a local OpenChoreo cluster. The two agents run as OpenChoreo **Agents**, Gitea, Lago and the support desk console run as **Components**, and Lago's database and Redis are **Resources**. OpenChoreo builds the two agents from this repository on GitHub, the way it builds a component. The OpenChoreo artifacts are kept in this folder, so the whole deployment lives next to the code.

It needs OpenChoreo with the agent kinds (`Agent`, `AgentType`) and their builds, which are on the `feat/agent-kinds` branch.

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
| desk-console | Component | `web-application`, from an image built on your machine |

The three types marked "ours" are in `platform/`. They are namespaced in `default`, so nothing cluster-wide changes:

- **`support-desk-agent` (AgentType)** - Runs an agent as a Deployment that serves A2A. See the next section.
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
- **A2A** - `LISTEN_ADDR` and `PUBLIC_URL` for the endpoint named `a2a`. `PUBLIC_URL` is the agent's in-cluster URL, so the agent card points to the same origin that callers resolve.
- **Env, files and secrets** - The workload's own env vars and files, with secrets from OpenBao through SecretReferences.

**How credentials flow:**

- **support-triage → gitea-mcp** - The agent sends no credentials. gitea-mcp holds the triage bot's Gitea token, a file mounted from the `support-desk-triage-bot` secret (`GITEA_ACCESS_TOKEN_FILE`), and acts as the triage bot in Gitea.
- **billing-agent → lago-mcp** - The agent sends no credentials. lago-mcp holds the Lago API key from the `support-desk-lago` secret (`LAGO_API_KEY`) and calls Lago with it.
- **support-triage → billing-agent** - Plain A2A inside the cluster, with no authentication yet.
- **desk-console → Gitea and Lago** - The support-inbox bot's Gitea token is a file mounted from the `support-desk-support-inbox` secret, and the Lago API key comes from `support-desk-lago`. The console only reads from Lago.

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
        url: https://github.com/VajiraPrabuddhaka/support-desk   # SOURCE_REPO
        revision:
          branch: main                                          # SOURCE_BRANCH
        appPath: /agents/support-triage
      docker:
        context: /
        filePath: /agents/support-triage/Dockerfile
```

A build is a WorkflowRun with the `openchoreo.dev/project` and `openchoreo.dev/agent` labels, which `make oc-agents` creates. It runs the following steps:

1. It checks out the repository at the commit of the run.
2. It builds `agents/<name>/Dockerfile`, with the root of the repository as the context, and pushes the image to the cluster's registry as `default-support-desk-<name>:v1-<commit>`.
3. It reads `agents/<name>/workload.yaml` and writes the Workload `<name>-workload` with the image it pushed. The Workload names the Agent as its owner (`owner.agentName`), and the descriptor's `prompt.md` (`valueFrom.path`) goes inline into it.

The Agent takes its workload from that Workload. Each new image cuts a new release, which autoDeploy binds to `development`. Until the first build writes the Workload, the Agent is not ready, with the reason `AwaitingBuild`.

**Key notes:**

- **Entrypoint** - A workload descriptor has no command, so each agent's Dockerfile builds only that agent and sets it as the `ENTRYPOINT`.
- **Model** - The model is a choice of the environment, not of the build. Each Agent's binding in `development` sets `OPENROUTER_MODEL` from `.env` with `workloadOverrides`, so changing the model needs no new build.
- **Credentials** - The descriptors reference secrets by name (`secretKeyRef`), as the values stay in OpenBao.

---

## Prerequisites

Before you begin, ensure you have:

- OpenChoreo running on k3d (kube context `k3d-openchoreo`), built from `feat/agent-kinds` and installed with the build plane (`--with-build`). A build writes an Agent's Workload with `occ workload create --agent`, so the cluster needs the `openchoreo-cli` image from that branch as well.
- The code on GitHub. OpenChoreo builds the agents from `SOURCE_REPO` and `SOURCE_BRANCH` in `.env` (this repository's `main` by default), so push your changes before Step 6.
- The Agent controller allowed to pin skills from the cluster's registry over HTTP:
  ```bash
  kubectl -n openchoreo-control-plane set env deployment/controller-manager \
    AGENT_SKILL_INSECURE_REGISTRIES=host.k3d.internal:10082
  ```
- Docker, Go 1.26 or later, `git`, `jq`, `python3`, `make` and `kubectl` on your machine
- A `.env` with your OpenRouter key: run `make env` and set `OPENROUTER_API_KEY` (see the main [README](../README.md))
- Memory for about 4 GB of extra pods. If the Docker Compose version of the support desk runs on the same Docker VM, stop it first with `make down`.

---

## Deploy

`make oc-up` runs all the steps below in order. Each step is also its own target, so you can run them one at a time and check the result in between.

### Step 1: Push the Images

```bash
make oc-images
```

This builds the console's image and one image per skill, and pushes them to the cluster's registry (`localhost:10082`, which the cluster pulls as `host.k3d.internal:10082`). OpenChoreo builds the agents itself in Step 6. Each tag is a hash of its sources, so a change gets a new tag.

### Step 2: Store the Secrets

```bash
make oc-secrets
```

This writes the OpenRouter key and Lago's keys from `.env` to OpenBao, under `secret/support-desk/`. The values go through stdin and never show up in a command line.

### Step 3: Apply the Platform Kinds

```bash
make oc-platform
```

### Step 4: Deploy Gitea, Lago and the MCP Servers

```bash
make oc-app
```

This creates the project, the secret references, `lago-db` and `lago-redis` (and points their bindings at their releases), the components, and starts the build of `lago-mcp`. It waits until Gitea and the Lago API are up. The first start takes a few minutes, as it pulls the images and runs Lago's migrations. On a fresh install, `gitea-mcp` stays in `ContainerCreating` until Step 5 stores the triage bot's token, as its pod mounts that token.

> [!NOTE]
> Each component file applies its ReleaseBinding and Workload before the Component. That way, autoDeploy finds the environment settings (resources, volume sizes, external URLs) in place and only fills in the release.

The build of `lago-mcp` takes a few more minutes, as it compiles Lago's MCP server from source. Until it finishes, `lago-mcp` runs the published image as a stand-in. The build writes its image into the Workload named `lago-mcp-workload`, which is why that Workload has this name: the build finds it and replaces only its image, and keeps the endpoint, the dependency and the command. Check the build with:

```bash
kubectl get workflowrun lago-mcp-build-01 -n default
```

### Step 5: Bootstrap Gitea and Lago

```bash
make oc-bootstrap
```

OpenChoreo has no one-off job type, so this runs from your machine:

1. It creates the Gitea admin account inside the Gitea pod.
2. It runs `tools/seed` against Gitea and the Lago API through port-forwards, which creates the five scenarios.
3. It writes the two bots' Gitea tokens to OpenBao, where the secret references read them: triage-bot's for gitea-mcp, and support-inbox's for the console.

### Step 6: Deploy the Agents

```bash
make oc-agents
```

This applies the two Agents and their bindings in `development`, with the skill images from Step 1. Then it starts a build of each agent, a WorkflowRun of the Agent's workflow pinned to the head commit of `SOURCE_BRANCH` (see [How the Agents Are Built](#how-the-agents-are-built)), and waits until both agents are ready. The first build takes a few minutes, as it downloads the Go modules. Once a build writes the Workload, the Agent controller pins the skills, cuts a release and binds it to `development`.

A run is named after its agent and commit, so running `make oc-agents` again for the same commit does not build again. To deploy a change to an agent, push it and run `make oc-agents` again. Check the builds with:

```bash
kubectl get workflowruns -n default -l openchoreo.dev/agent
```

### Step 7: Deploy the Console

```bash
make oc-console
```

This deploys the console as the `desk-console` Component, a `web-application` that runs the image from Step 1 with the command `console`. It reaches Gitea and the Lago API through endpoint dependencies, like any other component. support-triage is the exception: a Component cannot depend on an Agent yet, so the Workload sets `TRIAGE_AGENT_URL` to `http://support-triage:8001`, the agent's Service in the project's namespace. The agent's network policy lets in every pod of the project, so the call goes through.

The console's name is kept short on purpose. OpenChoreo shortens a longer component name in the external hostname, so `support-console`, for example, would show up as `support-conso`.

---

## Try It Out

Open the console at http://http-desk-console-development-default-1f3c1113.openchoreoapis.localhost:19080. Pick a ticket and click **Triage with AI**. The agent activity panel shows each step as the agents work, and the account panel shows the customer's invoices and coupons in Lago. **New request** files a new ticket the way a customer would. See [Step 3 of the main README](../README.md#step-3-triage-tickets-in-the-console) for the parts of the console.

You can also triage from the terminal. Triage a ticket:

```bash
make oc-triage T=1
```

Or triage every untriaged ticket:

```bash
make oc-triage
```

In a second terminal, follow what the agents do:

```bash
make oc-logs
```

`make oc-status` shows the bindings, the pods and these URLs:

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
├── platform/        the AgentType, ResourceType and ComponentType of the support desk
├── app/             the project, secret references, resources and components, the console included
├── agents/          the two Agents and their bindings (${...} values are filled in at deploy time)
├── build/           the WorkflowRun that builds lago-mcp
└── scripts/         the steps behind the oc-* make targets
```

Each agent's `Dockerfile` and `workload.yaml` are next to its code in `agents/<name>/` at the root of the repository, where the build reads them.

---

## Known Limits

- **Agent builds** - A build starts from a WorkflowRun, which `make oc-agents` creates. Agents do not build on a push yet (`autoBuild`).
- **Skills** - An Agent takes a skill only from an OCI image, so `make oc-images` builds and pushes the skills' images. Skills from Git are not supported yet.
- **Console build** - The console's image is built on your machine. A Component can build with `dockerfile-builder` too, so it can move to OpenChoreo the same way as the agents.
- **Bootstrap** - It runs from your machine, as there is no one-off job type.
- **Environments** - Only `development`. An Agent deploys to the first environment of its project's pipeline.
- **A2A** - The call between the agents is not authenticated yet.
- **Agent credentials** - The agents carry none for now. gitea-mcp and lago-mcp act with their own credentials for any caller in the project, and lago-mcp offers every Lago tool, as the tool allowlist lives in billing-agent.
- **Console to agent** - A Component cannot declare a dependency on an Agent yet, so the console's Workload sets support-triage's address by hand.
- **Console sign-in** - The console has no sign-in. Anyone who can open its URL can file and triage tickets, which is fine for a local cluster only.
- **Billing limits** - They live in the billing policy skill. Lago does not enforce them.
- **OpenRouter free models** - A free model allows 50 requests a day on an account without credits, and triaging one billing ticket takes about 15 across both agents. After the limit, calls fail with `429 Too Many Requests` until the next day (UTC). Credits raise the limit, or let you use a paid model.

---

## Clean Up

```bash
make oc-down
```

This removes the console, the agents and their builds, the components, the resources (with their data), the project and the secret references. The platform kinds in `platform/` and the values in OpenBao stay. Remove them with `kubectl delete -f openchoreo/platform/` and from OpenBao if you no longer need them.
