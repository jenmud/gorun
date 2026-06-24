generate:
	go tool goyacc -o parser/gorunfile.go -v parser/y.output parser/gorunfile.y

run:
	go run .

fix:
	go fix ./...

vendor:
	go mod tidy
	go mod vendor

test:
	go test ./...
