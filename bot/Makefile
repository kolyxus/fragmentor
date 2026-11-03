# Run from the bot/ directory. The recipes use no shell syntax, so they work
# both with make on Windows, which runs them through cmd.exe, and on Linux.

BIN := bin/fragmentor$(shell go env GOEXE)

# The server build, see DEPLOY.md. For an ARM server:
# make build-linux LINUX_ARCH=arm64
LINUX_ARCH ?= amd64
LINUX_BIN := bin/fragmentor-linux-$(LINUX_ARCH)

# The server for make deploy, an alias in ~/.ssh/config (see DEPLOY.md).
DEPLOY_HOST ?= fragmentor-vps

# ssh and scp for make deploy: on Windows, its own OpenSSH, as in PowerShell.
# It lives in System32\OpenSSH, which a 32-bit make, like the one from
# Chocolatey, does not see: Windows shows such a process SysWOW64 in place of
# System32, and make fails with "CreateProcess(NULL, scp ...) failed". To it
# the real System32 is Sysnative. Anywhere else ssh and scp come from PATH.
DEPLOY_SSH ?= $(or $(wildcard C:/Windows/Sysnative/OpenSSH/ssh.exe),ssh)
DEPLOY_SCP ?= $(or $(wildcard C:/Windows/Sysnative/OpenSSH/scp.exe),scp)

.DEFAULT_GOAL := build
.PHONY: run test lint build build-linux deploy

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

# Section 4 of DEPLOY.md in one command. Run by hand only: Claude Code is
# denied it. Make expands the whole recipe before running its first line, so
# the checks of the first two lines stop it before anything else: a working
# tree with uncommitted or untracked changes (the build would be "+dirty"
# and not what is committed), or no git. Then lint, tests and the server
# build, which stop it on a failure; then the binary and
# deploy/install-release.sh go to the server, and the script installs the
# binary there, see its header.
deploy: REVISION = $(shell git rev-parse HEAD)
deploy: CHANGES = $(strip $(shell git status --porcelain))
deploy:
	@$(if $(REVISION),,$(error make deploy: no git revision, run it from the repository with git in PATH))
	@$(if $(CHANGES),$(error make deploy: commit or stash the changes first: $(CHANGES)))
	$(MAKE) lint test build-linux
	$(DEPLOY_SCP) $(LINUX_BIN) deploy/install-release.sh $(DEPLOY_HOST):
	$(DEPLOY_SSH) $(DEPLOY_HOST) sh install-release.sh $(REVISION) $(notdir $(LINUX_BIN))
