.PHONY: vendor test build-otel build-go build

all: vendor build

vendor:
	go mod tidy && go mod vendor


test:
	go test -race ./...


build-otel:
	# CTI - compile time instrumentation
	go tool otelc go build -o builds/gorun-otel-CTI ./cmd/gorun && chmod +x builds/gorun-otel-CTI


build-go:
	go build -o builds/gorun ./cmd/gorun && chmod +x builds/gorun


build: build-go build-otel


-include ./example/Makefile
# up:   is imported from the examples Makefile above
# down: is imported from the examples Makefile above
# run:  is imported from the examples Makefile above
