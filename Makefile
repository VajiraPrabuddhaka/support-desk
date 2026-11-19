.DEFAULT_GOAL := help
COMPOSE := docker compose

.PHONY: help env up down reset ps logs triage ask-billing check

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

