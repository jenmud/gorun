.PHONY: up down run

# Directory of this Makefile
RECIPES_DIR := $(dir $(lastword $(MAKEFILE_LIST)))

up:
	docker compose -f $(RECIPES_DIR)docker-compose.yml up -d

down:
	docker compose -f $(RECIPES_DIR)docker-compose.yml down

run:
	@cd $(RECIPES_DIR) && go run ../cmd Gorunfile