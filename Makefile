fix:
	go fix ./...

vendor:
	go mod tidy
	go mod vendor

test:
	go test ./...
