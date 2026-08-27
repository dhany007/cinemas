.PHONY: up down ps logs build seed

ifneq (,$(wildcard .env))
include .env
export OMDB_API_KEY
endif

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

seed: up ## Create idempotent local cinemas, studios, and OMDb movies.
	@test -n "$(OMDB_API_KEY)" || (echo "OMDB_API_KEY is required; copy .env.example to .env and set the key."; exit 2)
	docker compose run --rm --build seed
