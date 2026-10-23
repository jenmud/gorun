.PHONY: up down run

# Directory of this Makefile, so targets work both here and when included from the repo root
EXAMPLE_DIR := $(dir $(lastword $(MAKEFILE_LIST)))

all: up

up:
	docker compose -f $(EXAMPLE_DIR)docker-compose.yml up -d

down:
	docker compose -f $(EXAMPLE_DIR)docker-compose.yml down

run:
	@cd $(EXAMPLE_DIR) && go run .. Gorunfile
