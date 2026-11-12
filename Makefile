.PHONY: vendor test build-otel build-go build templ-gen web web-dev examples

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


build-otel:
	# CTI - compile time instrumentation
	go tool otelc go build -o builds/gorun-otel-CTI ./cmd && chmod +x builds/gorun-otel-CTI


build-go:
	go build -o builds/gorun ./cmd && chmod +x builds/gorun


build: build-go build-otel

TEMPL := $(shell go env GOPATH)/bin/templ

templ-gen:
	$(TEMPL) generate ./cmd/web/...

# Override with `make web GORUN_ADDRESS=127.0.0.1:9090`.
GORUN_ADDRESS ?= :8080
# templ's proxy needs a host, so a bare ":port" proxies to localhost.
WEB_PROXY := http://$(if $(filter :%,$(GORUN_ADDRESS)),localhost)$(GORUN_ADDRESS)

web:
	GORUN_ADDRESS=$(GORUN_ADDRESS) go run ./cmd -serve

web-dev:
	GORUN_ADDRESS=$(GORUN_ADDRESS) $(TEMPL) generate -watch -cmd "go run ./cmd -serve" -proxy $(WEB_PROXY) ./cmd/web/...


-include ./recipes/Makefile
# up:   is imported from the examples Makefile above
# down: is imported from the examples Makefile above
# run:  is imported from the examples Makefile above
