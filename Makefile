.PHONY: up down ps logs build

up: ## Build and start the complete local stack in the background.
	docker compose up --build --detach

down: ## Stop the stack while preserving the PostgreSQL volume.
	docker compose down

ps: ## Show service status.
	docker compose ps

logs: ## Follow logs from every service.
	docker compose logs --follow

build: ## Build the API and web application images.
	docker compose build
