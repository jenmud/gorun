vendor:
	go mod tidy && go mod vendor


run:
	go run . Gorunfile


build-otel:
	# CTI - compile time instrumentation
	go tool otelc go build -o builds/gorun-otel-CTI . && chmod +x builds/gorun-otel-CTI


build-go:
	go build -o builds/gorun . && chmod +x builds/gorun


build: build-go build-otel
