# GeoGrid Support Desk: Two Go Agents over A2A and MCP

This repository shows how two agents written in Go work together over A2A, while each one uses its own MCP server to work on a real system. A support triage agent reads customer tickets in Gitea and asks a billing agent about billing tickets. The billing agent looks the customer up in Lago and takes the actions its policy allows.

Nothing is mocked. Gitea and Lago run locally in Docker with their official MCP servers, and the agents change real tickets and real invoices. Only the model calls leave your machine, to OpenRouter.

A small web console comes with it. You can see the tickets, file new ones and triage a ticket with one click, while each step of the agents' work shows up as it happens.

The same support desk also runs on OpenChoreo, with the agents as OpenChoreo Agents. OpenChoreo builds the agents and the console from this repository. See [openchoreo/README.md](openchoreo/README.md).

On OpenChoreo the two agents can also run on kagent, where every conversation with an agent runs in its own sandbox. That variant has the agents written again for kagent, and its own AgentType. See [kagent/README.md](kagent/README.md).

## Table of Contents

- [The Story](#the-story)
- [Architecture](#architecture)
- [Prompts and Skills](#prompts-and-skills)
- [Scenarios](#scenarios)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Talking to the Agents Directly](#talking-to-the-agents-directly)
- [Running the Agents Outside Docker](#running-the-agents-outside-docker)
- [Guardrails](#guardrails)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Repository Layout](#repository-layout)
- [Next Steps](#next-steps)

---

## The Story

GeoGrid is a made-up company that sells a geocoding API. Customers pay for the Growth plan (199 USD a month in advance, plus usage) and can buy add-ons such as an onboarding package. Billing runs on Lago.

Customer emails and web forms end up as issues in the private `geogrid/support` repository in Gitea, opened by the `support-inbox` bot. Two agents work on them:

- **support-triage** - Reads a ticket, sets a type and a priority label, and writes a triage note with a suggested reply. For billing tickets, it asks billing-agent first.
- **billing-agent** - Checks that the person who wrote in is the billing contact of the account, investigates invoices, payments and coupons in Lago, and returns its findings, what it changed, what needs a person, and a draft reply for the customer.

## Architecture

```
 console :3030 ──┐
                 ├──A2A──▶ support-triage :8001 ──MCP──▶ gitea-mcp :8091 ──▶ Gitea :3001
 make triage ────┘              │                        (issues, labels, comments)
                                │ A2A (billing_agent tool)
                                ▼
                           billing-agent :8002 ──MCP──▶ lago-mcp :8092 ──▶ Lago API :3000
                                                        (customers, invoices, coupons)
 both agents ──▶ OpenRouter
 console ──▶ Gitea (lists and files tickets) and the Lago API (reads accounts)
```

Both agents are built with [ADK Go](https://github.com/google/adk-go) v2. The model is reached through OpenRouter's OpenAI-compatible API (`model/openaimodel`), MCP tools come from `tool/mcptoolset`, and each agent is served over A2A with `server/adka2a`. support-triage calls billing-agent through `agent/remoteagent`, wrapped as a tool with `tool/agenttool`. This keeps support-triage in charge of the ticket, as billing-agent only answers.

**How credentials flow:**

- **support-triage → Gitea** - The agent sends no credentials. gitea-mcp holds the `triage-bot` user's Gitea token, which `seed` writes to `/secrets/triage-bot.token`, and acts as the triage bot, so Gitea decides what the bot may do. The token has the `write:issue`, `read:repository` and `read:user` scopes.
- **billing-agent → Lago** - The agent sends no credentials. lago-mcp holds the Lago API key and calls Lago with it.
- **support-triage → billing-agent** - Plain A2A over JSON-RPC, with no authentication yet. See [Next Steps](#next-steps).
- **console → Gitea** - The console lists and files tickets as the `support-inbox` bot, the same account that opens tickets from customer emails. Its token is in `/secrets/support-inbox.token`, with the same scopes as the triage bot's.
- **console → Lago** - The console reads the customer's account with the Lago API key. It changes nothing in Lago, as only billing-agent acts on invoices and coupons.

> [!NOTE]
> The agents carry no credentials of their own for now. This means anything that can reach gitea-mcp or lago-mcp acts with their credentials, the host ports 8091 and 8092 included. lago-mcp also offers such a caller every Lago tool, as the tool allowlist lives in billing-agent.

All host ports are bound to `127.0.0.1`.

| Service | Port | Purpose |
|---|---|---|
| `console` | 3030 | Support desk console (web UI) |
| `support-triage` | 8001 | Triage agent, A2A |
| `billing-agent` | 8002 | Billing agent, A2A |
| `gitea` | 3001 | Ticket system (web UI and API) |
| `gitea-mcp` | 8091 | Gitea MCP server, streamable HTTP at `/mcp` |
| `lago-api` | 3000 | Lago API |
| `lago-ui` | 3080 | Lago web UI |
| `lago-mcp` | 8092 | Lago MCP server, streamable HTTP at `/mcp` |
| `seed` | - | Creates the scenarios below, then exits |

## Prompts and Skills

Each agent's instructions are in two parts:

- **System prompt** - `agents/<name>/prompt.md` holds the agent's role and its steps.
- **Skills** - `agents/<name>/skills/<skill>/SKILL.md` holds the rules, in the [Agent Skills](https://agentskills.io) format. support-triage has `triage-rubric` (labels, priorities and the note format), and billing-agent has `billing-policy` (who may be told what, and the only changes allowed).

At startup the agent reads its prompt from `AGENT_SYSTEM_PROMPT_FILE` and its skills from `AGENT_SKILLS_DIR` when these are set, and adds every skill to its instructions, so the rules apply to every request. OpenChoreo mounts both: the prompt from the workload file that the Agent's `spec.systemPrompt` names, and the skills as pinned images. Without them, as in Docker Compose, the agent uses the copies built into its binary.

---

## Scenarios

The `seed` service creates five customers in Lago and one ticket for each in Gitea. Running it again creates nothing twice. The one exception is the duplicate invoice of scenario 1: once billing-agent voids it, the next seed run creates a new one, so the scenario can be played again.

| # | Ticket | Expected triage | What billing-agent should do |
|---|---|---|---|
| 1 | Bluefin Logistics was invoiced twice for onboarding (750 USD) and has not paid yet. They ask to cancel the duplicate and to waive next month's subscription. | `type/billing`, P2 | Find the duplicate invoice and void it. Leave the waiver to a person. |
| 2 | Kettle & Co asks what they owe and whether their API keys will stop working. | `type/billing`, P3 | List the unpaid invoices with amounts, due dates and PDF links. |
| 3 | Orbit Health asks for the LAUNCH20 discount that sales promised. | `type/billing`, P3 | Check the coupon rules and apply LAUNCH20. Flag that the first invoice was already issued at full price. |
| 4 | Tidewater Labs reports HTTP 500 from batch geocoding for large files. | `type/bug`, P2 or P3 | Not called. |
| 5 | Someone from a free-mail address asks for all of Granite Ventures' invoices and payment details. | `type/billing`, `status/needs-info` | Refuse to disclose anything, as the requester is not the billing contact on file. |

The agents decide on their own, so the exact labels and wording can differ between runs and models.

> [!NOTE]
> This is the free, self-hosted edition of Lago. Credit notes and recorded payments are paid features there (the API answers `403 feature_unavailable`), so invoices are marked paid directly, and billing-agent voids an unpaid duplicate and leaves refunds of paid invoices to Finance. Voiding invoices and applying coupons work in the free edition.

---

## Prerequisites

Before you begin, ensure you have:

- Docker with Docker Compose v2, and at least **6 GB RAM** for the Docker VM (Lago alone runs seven containers)
- An [OpenRouter](https://openrouter.ai) API key
- `make` and `openssl`
- Go 1.26 or later, only if you want to run the agents outside Docker

---

## Getting Started

### Step 1: Create the `.env` File

Run the following command:

```bash
make env
```

This copies `.env.example` to `.env` and generates every password, key and secret in it. Then set your OpenRouter key in `.env`:

```bash
OPENROUTER_API_KEY=sk-or-...
```

The default model is `anthropic/claude-haiku-5.5`, as it is cheap and handles tool calls well. Change `OPENROUTER_MODEL`, or set `TRIAGE_MODEL` and `BILLING_MODEL` to use a different model for each agent.

### Step 2: Start Everything

```bash
make up
```

The first run takes a while. Compose pulls the Gitea and Lago images, builds the agents, and builds the Lago MCP server from source. The published `getlago/lago-mcp-server` image is from November 2025, which is before the coupon, invoice and payment tools were added, so compose builds Lago's own Dockerfile at a pinned commit instead.

Compose starts the services in this order: Gitea and Lago, then `gitea-init` (creates the Gitea admin account), then `seed`, then the MCP servers and the agents. Check the progress with:

```bash
make ps
docker compose logs -f seed
```

### Step 3: Triage Tickets in the Console

Open the console at http://localhost:3030. It has the following parts:

- **Ticket queue** - Every ticket in `geogrid/support`, newest first, with filters for new, triaged and needs-info tickets.
- **Ticket** - The customer's message, the labels and the triage note. **Triage with AI** asks support-triage to triage the ticket, and **Redo triage** asks it to look at a triaged ticket again.
- **Agent activity** - Each step of the triage as it happens: support-triage reading the ticket, its request to billing-agent and billing-agent's answer, the labels it set and the note it wrote.
- **Account** - The customer's invoices and coupons in Lago. The panel refreshes when a triage ends, so you can see a duplicate invoice turn voided or a coupon show up.

**New request** files a ticket the way a customer would through the web form, and **Fill an example** fills in one of three requests that match the customers in Lago.

The console calls support-triage over A2A, the same way as any other client. It asks for every step of the agent's work as a separate A2A artifact, so it can show each step while the agent is still working.

You can also triage from the terminal. Triage one ticket:

```bash
make triage T=1
```

Or triage every untriaged ticket:

```bash
make triage
```

In a second terminal, follow what the agents do. Every tool call and its result is logged:

```bash
make logs
```

### Step 4: Check the Results

The console shows the labels and the triage note on each ticket, and the account panel shows what billing-agent changed. You can also check the results in Gitea and Lago:

- **Gitea** - Open http://localhost:3001/geogrid/support/issues and sign in with `GITEA_ADMIN_USER` and `GITEA_ADMIN_PASSWORD` from `.env`. Each triaged ticket has labels and a triage note from `triage-bot`.
- **Lago** - Open http://localhost:3080 and sign in with `LAGO_ADMIN_EMAIL` and `LAGO_ADMIN_PASSWORD`. For scenario 1, one of Bluefin Logistics' two onboarding invoices is voided. For scenario 3, Orbit Health has LAUNCH20 applied.

To start over with fresh data:

```bash
make reset
make up
```

---

## Talking to the Agents Directly

You can ask billing-agent a question without going through triage:

```bash
make ask-billing Q="Account ACC-1077, requester accounts@kettle.example.com: what do we owe?"
```

Each agent serves its A2A agent card:

```bash
curl -s http://localhost:8001/.well-known/agent-card.json
curl -s http://localhost:8002/.well-known/agent-card.json
```

The `ask` tool is a small A2A client in `tools/ask`. You can also run it from the host:

```bash
go run ./tools/ask -agent http://localhost:8002 "Account ACC-1042, requester billing@bluefin.example.com: why were we invoiced twice?"
```

> [!NOTE]
> Inside Docker, an agent card advertises the service name (e.g., `http://billing-agent:8002/a2a`), which does not resolve from the host. `ask` sends the calls to the host it fetched the card from, so it works both inside and outside Docker.

---

## Running the Agents Outside Docker

This is useful when you change the agent code. Keep Gitea, Lago and the MCP servers in Docker, and run the agents with `go run`.

1. Stop the agent containers:

   ```bash
   docker compose stop support-triage billing-agent
   ```

2. Start billing-agent in one terminal:

   ```bash
   set -a; source .env; set +a
   go run ./agents/billing-agent
   ```

3. Start support-triage in another terminal:

   ```bash
   set -a; source .env; set +a
   go run ./agents/support-triage
   ```

4. Triage a ticket:

   ```bash
   go run ./tools/ask "Triage ticket #1."
   ```

The defaults of every agent setting point to the local ports, and the agents need no Gitea or Lago credentials, so nothing else needs to be set.

The console works the same way. The console container cannot reach agents that run on the host, so stop it with `docker compose stop console`. The console needs the support-inbox bot's token, so copy it out of the volume that `seed` wrote it to, then start the console and open http://localhost:8080:

```bash
mkdir -p .secrets
docker compose cp seed:/secrets/support-inbox.token .secrets/
set -a; source .env; set +a
go run ./console
```

> [!NOTE]
> Do not run `tools/seed` from the host while compose is up. The seed replaces a bot's token when it cannot use the one in its own folder, which leaves gitea-mcp and the console container with tokens that Gitea no longer accepts.

---

## Guardrails

The agents act on real data, so each one has limits beyond its prompt:

- **Tool allowlists** - Each agent exposes only some of its MCP server's tools to the model. support-triage gets `list_issues`, `issue_read`, `issue_write`, `search_issues` and `label_read`, and gitea-mcp also runs with the same list (`GITEA_TOOLS`). billing-agent gets the read tools of Lago plus two write tools: `void_invoice` and `apply_coupon`. It cannot create or delete customers, invoices, subscriptions, credit notes or coupons.
- **Write methods** - `issue_write` can also create, edit and close issues and delete comments. support-triage refuses every method except `add_labels` and `add_comment` before the call reaches Gitea, and refuses calls to any other repository (`guardTools` in `agents/support-triage/main.go`).
- **Gitea permissions** - The triage bot is a collaborator on the support repository only, and its token cannot touch anything outside issues.
- **Billing policy** - The billing rules are in the `billing-policy` skill (`agents/billing-agent/skills/billing-policy/SKILL.md`): verify the requester's email first, void only a clear, unpaid duplicate up to 1,000 USD, apply only LAUNCH20 and only within 30 days of signing up, and hand everything else to a person.

> [!IMPORTANT]
> The billing limits live only in the agent's instructions. Lago would void any unpaid invoice if the model asked it to, so the limits depend on the model following its instructions. This is a good place to test how well a model sticks to a policy, and a reason to move such limits into code or into the callee later.

---

## Configuration

All settings come from `.env`. The agents read the following variables:

| Variable | Agent | Default | Purpose |
|---|---|---|---|
| `OPENROUTER_API_KEY` | both | (required) | OpenRouter API key |
| `OPENROUTER_MODEL` | both | `anthropic/claude-haiku-5.5` | Model for both agents |
| `TRIAGE_MODEL` / `BILLING_MODEL` | one each | `OPENROUTER_MODEL` | Model for one agent |
| `MAX_OUTPUT_TOKENS` | both | `4096` | Cap on one model reply. Without it, OpenRouter reserves credit for the model's full output limit and refuses the call with 402 when the account cannot cover that. |
| `OPENROUTER_BASE_URL` | both | `https://openrouter.ai/api/v1` | Any OpenAI-compatible endpoint works |
| `LISTEN_ADDR` / `PUBLIC_URL` | both | `:8001`, `:8002` | Listen address, and the base URL written into the agent card |
| `SUPPORT_REPO` | triage | `geogrid/support` | The Gitea repository with the tickets |
| `GITEA_MCP_URL` | triage | `http://localhost:8091/mcp` | Gitea MCP server |
| `BILLING_AGENT_URL` | triage | `http://localhost:8002` | Where to find billing-agent's agent card |
| `LAGO_MCP_URL` | billing | `http://localhost:8092/mcp` | Lago MCP server |

The agents need no Gitea or Lago credentials. gitea-mcp reads the triage bot's token from the file in `GITEA_ACCESS_TOKEN_FILE`, and lago-mcp reads `LAGO_API_KEY`.

The console reads the following variables. Compose sets all of them, and `CONSOLE_PORT` in `.env` picks the host port (3030 by default).

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Listen address |
| `SUPPORT_REPO` | `geogrid/support` | The Gitea repository with the tickets |
| `GITEA_URL` | `http://localhost:3001` | Gitea, as the console reaches it |
| `GITEA_PUBLIC_URL` | (none) | Gitea, as your browser reaches it, for the "Open in Gitea" links. Needed when `GITEA_URL` is an address the browser cannot open, such as `http://gitea:3000`. |
| `GITEA_TOKEN_FILE` | `.secrets/support-inbox.token` | The support-inbox bot's token, read on every request |
| `TRIAGE_AGENT_URL` | `http://localhost:8001` | Where to find support-triage's agent card |
| `LAGO_API_URL` | `http://localhost:3000/api/v1` | Lago's REST API |
| `LAGO_API_KEY` | (none) | Lago API key. Without it, the account panel stays empty. |
| `LAGO_PUBLIC_URL` | (none) | The Lago UI, as your browser reaches it, for the "Open in Lago" links |

---

## Troubleshooting

**`402 Payment Required` from OpenRouter:**

OpenRouter checks, before each call, that the account can pay for the prompt plus the largest reply the call allows. The agents cap a reply at `MAX_OUTPUT_TOKENS` (4096), but a triage conversation also carries large tool results from Gitea and Lago, so an account with little or no credit runs out partway through. Add credits at https://openrouter.ai/settings/credits. A few dollars cover many runs with `anthropic/claude-haiku-5.5`.

**A ticket was triaged badly or partly:**

The triage agent skips tickets that already have a status label or its triage note. In the console, **Redo triage** asks it to triage such a ticket again. To try again with fresh data, run `make reset` and `make up`. This takes about a minute once the images are built.

**The console shows `support-triage · offline`:**

The console checks support-triage's agent card every 15 seconds. Right after `make up`, the agent may still be starting. If it stays offline, check the agent with `make logs`.

**`agent card ... uses plain http`:**

support-triage accepts plain http to billing-agent only when `A2A_ALLOW_HTTP=true`, which compose sets for the private Docker network. Outside Docker, `localhost` is always accepted.

---

## Repository Layout

```
.
├── agents/
│   ├── support-triage/     main.go, prompt.md, skills/triage-rubric, Dockerfile, workload.yaml
│   └── billing-agent/      main.go, prompt.md, skills/billing-policy, Dockerfile, workload.yaml
├── console/                the support desk console: a Go server, its web page, Dockerfile, workload.yaml
├── internal/
│   ├── agentkit/           shared code: model, instructions, MCP, A2A server, tool logging
│   └── tickets/            the ticket format that the seed and the console share
├── tools/
│   ├── seed/               creates the scenarios in Gitea and Lago
│   └── ask/                a small A2A client
├── openchoreo/             the OpenChoreo deployment (see openchoreo/README.md)
├── kagent/                 the two agents for kagent, with their OpenChoreo kinds (see kagent/README.md)
├── infra/gitea/            creates the Gitea admin account
├── scripts/init-env.sh     creates .env with generated secrets
├── compose.yaml            agents, console, MCP servers, Gitea and Lago
├── Dockerfile              one image with the five Go binaries
└── Makefile
```

The `Dockerfile` and `workload.yaml` of each agent and of the console are for OpenChoreo, which builds them from these. Docker Compose uses the `Dockerfile` at the root.

---

## Next Steps

- **Give the agents an identity** - The agents carry no credentials today, and the MCP servers act with their own for any caller. A first step is to have each agent prove who it is to the MCP servers, so every call can be traced to the agent that made it and limited to what that agent may do.
- **Authenticate the A2A call** - billing-agent accepts any caller today. A first step is to have support-triage present its own identity and have billing-agent check it.
- **Act on behalf of the person** - Today gitea-mcp acts as `triage-bot` and lago-mcp acts as the whole Lago organization, whichever agent calls them. Passing along who asked for the triage would let each system record and limit actions per person.
- **Move limits out of the prompt** - The 1,000 USD limit and the duplicate check could be enforced in billing-agent's code, in a proxy in front of lago-mcp, or by a Lago API key with fewer permissions.
- **Trigger triage automatically** - A Gitea webhook on new issues could call support-triage, so tickets are triaged as they arrive.
