.PHONY: lint format test build mock ci

lint:
	go vet ./...

format:
	gofmt -l .
	goimports -l .

test:
	go test ./...

build:
	go build ./...

mock:
	mockery

ci: format lint test build
