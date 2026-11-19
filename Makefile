.DEFAULT_GOAL := help
COMPOSE := docker compose

.PHONY: help env up down reset ps logs triage ask-billing check \
	oc-up oc-images oc-secrets oc-platform oc-app oc-bootstrap oc-agents oc-console \
	oc-status oc-logs oc-triage oc-ask-billing oc-down

help: ## Show the targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z-]+:.*## / { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

env: ## Create .env with generated secrets (keeps an existing .env)
	@./scripts/init-env.sh

up: env ## Build, start everything and seed the scenarios
	$(COMPOSE) up -d --build
	@echo
	@echo "Console:         http://localhost:$${CONSOLE_PORT:-3030}  (tickets, triage, agent activity)"
	@echo "Gitea (tickets): http://localhost:$${GITEA_PORT:-3001}  (admin user and password in .env)"
	@echo "Lago (billing):  http://localhost:$${LAGO_UI_PORT:-3080}  (LAGO_ADMIN_EMAIL and LAGO_ADMIN_PASSWORD in .env)"

ps: ## List the services and their state
	$(COMPOSE) ps -a

logs: ## Follow the two agents' logs (tool calls included)
	$(COMPOSE) logs -f support-triage billing-agent

triage: ## Triage ticket T (make triage T=1), or every untriaged ticket
	$(COMPOSE) run --rm ask "$(if $(T),Triage ticket #$(T).,Triage all untriaged tickets.)"

ask-billing: ## Ask billing-agent directly: make ask-billing Q="..."
	$(COMPOSE) run --rm ask -agent http://billing-agent:8002 "$(Q)"

down: ## Stop everything and keep the data
	$(COMPOSE) down

reset: ## Stop everything and delete all data (tickets, billing, tokens)
	$(COMPOSE) down -v

check: ## Format check, vet, test and build the Go code
	@test -z "$$(gofmt -l agents console internal tools)" || { gofmt -l agents console internal tools; exit 1; }
	go vet ./...
	go test ./...
	go build ./...

# ---------------------------------------------------------------- OpenChoreo
# The same support desk on OpenChoreo (kube context k3d-openchoreo). See
# openchoreo/README.md. oc-up runs every step in order.
OC := ./openchoreo/scripts

oc-up: oc-images oc-secrets oc-platform oc-app oc-bootstrap oc-agents oc-console ## OpenChoreo: deploy everything, step by step

oc-images: ## OpenChoreo: build and push the console's and skills' images
	$(OC)/push-images.sh

oc-secrets: ## OpenChoreo: write the secrets from .env to OpenBao
	$(OC)/put-secrets.sh

oc-platform: ## OpenChoreo: apply the AgentType, ResourceType and ComponentType
	kubectl apply -f openchoreo/platform/

oc-app: ## OpenChoreo: deploy the project, Gitea, Lago and the MCP servers
	$(OC)/deploy-app.sh

oc-bootstrap: ## OpenChoreo: create the Gitea admin, seed, store the bots' tokens
	$(OC)/bootstrap.sh

oc-agents: ## OpenChoreo: build billing-agent and support-triage from GitHub, deploy them
	$(OC)/deploy-agents.sh

oc-console: ## OpenChoreo: deploy the support desk console
	$(OC)/deploy-console.sh

oc-status: ## OpenChoreo: show bindings, pods and URLs
	$(OC)/status.sh

oc-logs: ## OpenChoreo: follow the two agents' logs (tool calls included)
	$(OC)/logs.sh

oc-triage: ## OpenChoreo: triage ticket T (make oc-triage T=1), or all untriaged
	$(OC)/ask.sh support-triage "$(if $(T),Triage ticket #$(T).,Triage all untriaged tickets.)"

oc-ask-billing: ## OpenChoreo: ask billing-agent directly: make oc-ask-billing Q="..."
	$(OC)/ask.sh billing-agent "$(Q)"

oc-down: ## OpenChoreo: remove the support desk (keeps platform kinds and OpenBao values)
	$(OC)/teardown.sh
