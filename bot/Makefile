# Run from the bot/ directory. The recipes use no shell syntax, so they work
# both with make on Windows, which runs them through cmd.exe, and on Linux.

BIN := bin/fragmentor$(shell go env GOEXE)

# The server build, see DEPLOY.md. For an ARM server:
# make build-linux LINUX_ARCH=arm64
LINUX_ARCH ?= amd64
LINUX_BIN := bin/fragmentor-linux-$(LINUX_ARCH)

.DEFAULT_GOAL := build
.PHONY: run test lint build build-linux

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

# A static binary: without cgo it does not depend on the server's libc.
# Target-specific exports set the variables for go itself, whatever the shell.
# -s drops the symbol table and DWARF; panics still print full stack traces.
build-linux: export CGO_ENABLED := 0
build-linux: export GOOS := linux
build-linux: export GOARCH := $(LINUX_ARCH)
build-linux:
	go build -trimpath -ldflags=-s -o $(LINUX_BIN) ./cmd/bot
