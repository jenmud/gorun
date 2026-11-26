.PHONY: vendor test build-otel build-go build templ-gen web web-dev examples model

all: vendor build

vendor:
	go mod tidy && go mod vendor


test:
	go test -race ./...

# The library examples are modules of their own (examples/*/go.mod, built against this
# checkout), so `go test ./...` above doesn't reach them.
examples:
	cd examples/rollout && go vet ./... && go build -o /dev/null .
	cd examples/servicedeck && go vet ./... && go build -o /dev/null .


# the version -version reports: the tag, or the tag, commits since and commit, e.g. v0.32.0-3-g1a2b3c4
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -X main.version=$(VERSION)

build-otel:
	# CTI - compile time instrumentation
	go tool otelc go build -ldflags "$(LDFLAGS)" -o builds/gorun-otel-CTI ./cmd && chmod +x builds/gorun-otel-CTI


build-go:
	go build -ldflags "$(LDFLAGS)" -o builds/gorun ./cmd && chmod +x builds/gorun


build: build-go build-otel

TEMPL := $(shell go env GOPATH)/bin/templ

templ-gen:
	$(TEMPL) generate ./cmd/web/...

# Override with `make web GORUN_ADDRESS=127.0.0.1:9090`.
GORUN_ADDRESS ?= :8080
# templ's proxy needs a host, so a bare ":port" proxies to localhost.
WEB_PROXY := http://$(if $(filter :%,$(GORUN_ADDRESS)),localhost)$(GORUN_ADDRESS)

# The assistant's model. Empty is gorun's built-in one, Qwen3 4B Instruct, downloaded on the first
# question. The version fine-tuned on gorun (training/) learned the rules from before tasks had to
# have a check and a verify, so its tasks are refused until it is retrained; try it with
# `make web-dev GORUN_LLM_MODEL=$(FINETUNED_MODEL)`, after `make model` pulls it ahead.
GORUN_LLM_MODEL ?=
FINETUNED_MODEL := https://huggingface.co/jenmud/gorun-qwen3-4b-GGUF/resolve/main/gorun-qwen3-4b-q4_k_m.gguf
# where gorun keeps downloaded models, as cmd/websrv's llmDir finds it
GORUN_CACHE_DIR ?= $(if $(filter Darwin,$(shell uname -s)),$(HOME)/Library/Caches,$(or $(XDG_CACHE_HOME),$(HOME)/.cache))/gorun
MODEL_FILE := $(GORUN_CACHE_DIR)/llm/models/$(notdir $(FINETUNED_MODEL))

model: $(MODEL_FILE)

# through a .part file, so an interrupted download is never used
$(MODEL_FILE):
	mkdir -p $(dir $@)
	curl -fL --progress-bar -o $@.part $(FINETUNED_MODEL)
	mv $@.part $@

web:
	GORUN_ADDRESS=$(GORUN_ADDRESS) $(if $(GORUN_LLM_MODEL),GORUN_LLM_MODEL=$(GORUN_LLM_MODEL)) go run ./cmd -serve

web-dev:
	GORUN_ADDRESS=$(GORUN_ADDRESS) $(if $(GORUN_LLM_MODEL),GORUN_LLM_MODEL=$(GORUN_LLM_MODEL)) $(TEMPL) generate -watch -cmd "go run ./cmd -serve" -proxy $(WEB_PROXY) ./cmd/web/...


-include ./recipes/Makefile
# up:   is imported from the examples Makefile above
# down: is imported from the examples Makefile above
# run:  is imported from the examples Makefile above
