.PHONY: vendor test build-otel build-go build templ-gen web web-dev

all: vendor build

vendor:
	go mod tidy && go mod vendor


test:
	go test -race ./...


build-otel:
	# CTI - compile time instrumentation
	go tool otelc go build -o builds/gorun-otel-CTI ./cmd && chmod +x builds/gorun-otel-CTI


build-go:
	go build -o builds/gorun ./cmd && chmod +x builds/gorun


build: build-go build-otel

TEMPL := $(shell go env GOPATH)/bin/templ

templ-gen:
	$(TEMPL) generate ./cmd/web/...

web:
	go run ./cmd -web

web-dev:
	$(TEMPL) generate -watch -cmd "go run ./cmd -web" -proxy http://localhost:8080 ./cmd/web/...


-include ./example/Makefile
# up:   is imported from the examples Makefile above
# down: is imported from the examples Makefile above
# run:  is imported from the examples Makefile above
