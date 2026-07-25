.PHONY: lint format test build mock ci

lint:
	go vet ./...

format:
	gofmt -l .
	go run golang.org/x/tools/cmd/goimports@v0.48.0 -l .

test:
	go test ./...

build:
	go build ./...

mock:
	mockery

ci: format lint test build
