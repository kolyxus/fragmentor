# Run from the bot/ directory. The recipes use no shell syntax, so they work
# both with make on Windows, which runs them through cmd.exe, and on Linux.

BIN := bin/fragmentor$(shell go env GOEXE)

.DEFAULT_GOAL := build
.PHONY: run test lint build

run:
	go run ./cmd/bot

test:
	go test ./...

# Fails if any file needs gofmt, then runs go vet.
lint:
	@$(if $(strip $(shell gofmt -l .)),$(error gofmt needed for: $(shell gofmt -l .)))
	go vet ./...

build:
	go build -trimpath -o $(BIN) ./cmd/bot
