# GeoGrid Support Desk: Two Go Agents over A2A and MCP

This repository shows how two agents written in Go work together over A2A, while each one uses its own MCP server to work on a real system. A support triage agent reads customer tickets in Gitea and asks a billing agent about billing tickets. The billing agent looks the customer up in Lago and takes the actions its policy allows.

Nothing is mocked. Gitea and Lago run locally in Docker with their official MCP servers, and the agents change real tickets and real invoices. Only the model calls leave your machine, to OpenRouter.

## Table of Contents

- [The Story](#the-story)
- [Architecture](#architecture)
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
 you ── make triage T=1 ──A2A──▶ support-triage :8001 ──MCP──▶ gitea-mcp :8091 ──▶ Gitea :3001
                                      │                          (issues, labels, comments)
                                      │ A2A (billing_agent tool)
                                      ▼
                                 billing-agent :8002 ──MCP──▶ lago-mcp :8092 ──▶ Lago API :3000
                                                              (customers, invoices, coupons)
 both agents ──▶ OpenRouter
```

Both agents are built with [ADK Go](https://github.com/google/adk-go) v2. The model is reached through OpenRouter's OpenAI-compatible API (`model/openaimodel`), MCP tools come from `tool/mcptoolset`, and each agent is served over A2A with `server/adka2a`. support-triage calls billing-agent through `agent/remoteagent`, wrapped as a tool with `tool/agenttool`. This keeps support-triage in charge of the ticket, as billing-agent only answers.

**How credentials flow:**

- **support-triage → Gitea** - The agent reads the `triage-bot` user's Gitea token from `/secrets/triage-bot.token` and sends it with every MCP request. gitea-mcp holds no token of its own and passes the agent's token to Gitea, so Gitea decides what the triage bot may do. The token has the `write:issue`, `read:repository` and `read:user` scopes.
- **billing-agent → Lago** - The agent sends the Lago API key in the `X-Lago-Api-Key` header with every MCP request. lago-mcp holds no key of its own.
- **support-triage → billing-agent** - Plain A2A over JSON-RPC, with no authentication yet. See [Next Steps](#next-steps).

All host ports are bound to `127.0.0.1`.

| Service | Port | Purpose |
|---|---|---|
| `support-triage` | 8001 | Triage agent, A2A |
| `billing-agent` | 8002 | Billing agent, A2A |
| `gitea` | 3001 | Ticket system (web UI and API) |
| `gitea-mcp` | 8091 | Gitea MCP server, streamable HTTP at `/mcp` |
| `lago-api` | 3000 | Lago API |
| `lago-ui` | 3080 | Lago web UI |
| `lago-mcp` | 8092 | Lago MCP server, streamable HTTP at `/mcp` |
| `seed` | - | Creates the scenarios below, then exits |

---

## Scenarios

The `seed` service creates five customers in Lago and one ticket for each in Gitea. Running it again creates nothing twice.

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

### Step 3: Triage the Tickets

Triage one ticket:

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

2. Seed from the host, so the triage bot's token is written to `.secrets/triage-bot.token`:

   ```bash
   set -a; source .env; set +a
   go run ./tools/seed
   ```

3. Start billing-agent in one terminal:

   ```bash
   set -a; source .env; set +a
   go run ./agents/billing-agent
   ```

4. Start support-triage in another terminal:

   ```bash
   set -a; source .env; set +a
   go run ./agents/support-triage
   ```

5. Triage a ticket:

   ```bash
   go run ./tools/ask "Triage ticket #1."
   ```

The defaults of every agent setting point to the local ports, so nothing else needs to be set.

---

## Guardrails

The agents act on real data, so each one has limits beyond its prompt:

- **Tool allowlists** - Each agent exposes only some of its MCP server's tools to the model. support-triage gets `list_issues`, `issue_read`, `issue_write`, `search_issues` and `label_read`, and gitea-mcp also runs with the same list (`GITEA_TOOLS`). billing-agent gets the read tools of Lago plus two write tools: `void_invoice` and `apply_coupon`. It cannot create or delete customers, invoices, subscriptions, credit notes or coupons.
- **Write methods** - `issue_write` can also create, edit and close issues and delete comments. support-triage refuses every method except `add_labels` and `add_comment` before the call reaches Gitea, and refuses calls to any other repository (`guardTools` in `agents/support-triage/main.go`).
- **Gitea permissions** - The triage bot is a collaborator on the support repository only, and its token cannot touch anything outside issues.
- **Billing policy** - The billing rules are in `agents/billing-agent/prompt.md`: verify the requester's email first, void only a clear, unpaid duplicate up to 1,000 USD, apply only LAUNCH20 and only within 30 days of signing up, and hand everything else to a person.

> [!IMPORTANT]
> The billing limits live only in the prompt. Lago would void any unpaid invoice if the model asked it to, so the limits depend on the model following its instructions. This is a good place to test how well a model sticks to a policy, and a reason to move such limits into code or into the callee later.

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
| `GITEA_TOKEN_FILE` | triage | `.secrets/triage-bot.token` | The triage bot's token, read on every request |
| `BILLING_AGENT_URL` | triage | `http://localhost:8002` | Where to find billing-agent's agent card |
| `LAGO_MCP_URL` | billing | `http://localhost:8092/mcp` | Lago MCP server |
| `LAGO_API_KEY` | billing | (required) | Lago API key, sent with every MCP request |

---

## Troubleshooting

**`402 Payment Required` from OpenRouter:**

OpenRouter checks, before each call, that the account can pay for the prompt plus the largest reply the call allows. The agents cap a reply at `MAX_OUTPUT_TOKENS` (4096), but a triage conversation also carries large tool results from Gitea and Lago, so an account with little or no credit runs out partway through. Add credits at https://openrouter.ai/settings/credits. A few dollars cover many runs with `anthropic/claude-haiku-5.5`.

**A ticket was triaged badly or partly:**

The triage agent skips tickets that already have a status label or its triage note. To try again with fresh data, run `make reset` and `make up`. This takes about a minute once the images are built.

**`agent card ... uses plain http`:**

support-triage accepts plain http to billing-agent only when `A2A_ALLOW_HTTP=true`, which compose sets for the private Docker network. Outside Docker, `localhost` is always accepted.

---

## Repository Layout

```
.
├── agents/
│   ├── support-triage/     main.go and prompt.md
│   └── billing-agent/      main.go and prompt.md
├── internal/agentkit/      shared code: model, MCP connection, A2A server, tool logging
├── tools/
│   ├── seed/               creates the scenarios in Gitea and Lago
│   └── ask/                a small A2A client
├── infra/gitea/            creates the Gitea admin account
├── scripts/init-env.sh     creates .env with generated secrets
├── compose.yaml            agents, MCP servers, Gitea and Lago
├── Dockerfile              one image with the four Go binaries
└── Makefile
```

---

## Next Steps

- **Authenticate the A2A call** - billing-agent accepts any caller today. A first step is to have support-triage present its own identity and have billing-agent check it.
- **Act on behalf of the person** - Today support-triage acts as `triage-bot` and billing-agent acts as the whole Lago organization. Passing along who asked for the triage would let each system record and limit actions per person.
- **Move limits out of the prompt** - The 1,000 USD limit and the duplicate check could be enforced in billing-agent's code, in a proxy in front of lago-mcp, or by a Lago API key with fewer permissions.
- **Trigger triage automatically** - A Gitea webhook on new issues could call support-triage, so tickets are triaged as they arrive.
