# Makefile
# Simple, portable Makefile that:
# - Bootstraps a pinned Go toolchain locally under .toolchain/go
# - Builds the rdapctl binary to ./bin/rdapctl
# - Runs tests and installs to PREFIX/bin

SHELL := /bin/bash

# --- Config ------------------------------------------------------------------

GOVERSION ?= 1.24.2

# Map uname -s / -m to Go's archive triplets
UNAME_S := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_M := $(shell uname -m)
# Normalize ARCH
ifeq ($(UNAME_M),x86_64)
  GOARCH := amd64
else ifeq ($(UNAME_M),aarch64)
  GOARCH := arm64
else ifeq ($(UNAME_M),arm64)
  GOARCH := arm64
else
  GOARCH := $(UNAME_M)
endif

# Adjust darwin naming
ifeq ($(UNAME_S),darwin)
  GOOS := darwin
else ifeq ($(UNAME_S),linux)
  GOOS := linux
else
  $(error Unsupported OS: $(UNAME_S))
endif

TOOLCHAIN_DIR := .toolchain
GODIR         := $(TOOLCHAIN_DIR)/go
GOURL         := https://go.dev/dl/go$(GOVERSION).$(GOOS)-$(GOARCH).tar.gz
GOTAR         := $(TOOLCHAIN_DIR)/go$(GOVERSION).$(GOOS)-$(GOARCH).tar.gz
GO            := $(GODIR)/bin/go

BIN_DIR := bin
BIN     := $(BIN_DIR)/rdapctl

PREFIX  ?= /usr/local

PKG := ./...
CMD := ./cmd/rdapctl

# --- Phonies -----------------------------------------------------------------

.PHONY: all bootstrap tidy deps build test install clean doctor env

all: build

# Download & extract a local Go toolchain (no sudo, no system pollution)
bootstrap:
	@mkdir -p $(TOOLCHAIN_DIR)
	@echo ">> downloading Go $(GOVERSION) for $(GOOS)/$(GOARCH)"
	@curl -sSL "$(GOURL)" -o "$(GOTAR)"
	@echo ">> unpacking to $(GODIR)"
	@rm -rf "$(GODIR)"
	@tar -C "$(TOOLCHAIN_DIR)" -xzf "$(GOTAR)"
	@echo ">> go installed to $(GODIR)"
	@$(GO) version

# Use system go if you prefer: `GO=go make build`
deps tidy:
	@$(GO) mod tidy

build: $(BIN)

$(BIN): $(CMD) go.mod
	@mkdir -p $(BIN_DIR)
	@echo ">> building $@"
	@$(GO) build -o $(BIN) $(CMD)
	@echo ">> built $(BIN)"

test:
	@$(GO) test -v $(PKG)

install: $(BIN)
	@echo ">> installing to $(PREFIX)/bin"
	@install -d "$(PREFIX)/bin"
	@install -m 0755 "$(BIN)" "$(PREFIX)/bin/rdapctl"
	@echo ">> installed $(PREFIX)/bin/rdapctl"

doctor:
	@echo "OS: $(GOOS)  ARCH: $(GOARCH)"
	@echo "Go tool: $(GO)"
	@$(GO) version

env:
	@echo "export PATH=$(GODIR)/bin:\$$PATH"

clean:
	@rm -rf "$(BIN_DIR)" "$(TOOLCHAIN_DIR)"

