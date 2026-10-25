# SPDX-License-Identifier: MIT
# go-modbus — developer Makefile
#
# Tabs are required by GNU make. The whitespace rules below pin sane
# shell behaviour so a failing recipe step actually aborts the target
# instead of silently moving on.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

GO            ?= go
GOFUMPT       ?= gofumpt
GOIMPORTS     ?= goimports
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK   ?= govulncheck

MODULE := github.com/SukramJ/go-modbus

.PHONY: help
help: ## show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Tool versions, pinned to match .github/workflows/ci.yml. A local gate is
# only usable if it reports what CI reports, so the two lists must be bumped
# together — raise both, run `make check`, and fix or justify whatever the
# new release finds in the same change.
#
# gofumpt is the sharpest case: it is a formatter, so an upstream release
# rewraps untouched code. On @latest that turns CI red on a diff that did
# not cause it, and leaves a developer's clean local run meaningless.
#
# govulncheck is pinned for the same reason even though CI does not run it
# yet: `make vuln` is a gate developers feel. Pinning the binary does not
# freeze the answer it gives — the vulnerability database is resolved at run
# time, so a new advisory still turns the gate red without a commit here.
#
# goimports stays on @latest deliberately: it has no gate of its own,
# gofumpt is the formatting authority.
GOFUMPT_VERSION       ?= v0.12.0
GOLANGCI_LINT_VERSION ?= v2.14.0
GOVULNCHECK_VERSION   ?= v1.8.0

.PHONY: setup
setup: hooks ## install developer tooling (gofumpt, goimports, golangci-lint, govulncheck) + git hooks
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: hooks
hooks: ## point git at the tracked hooks in .githooks/ (blocks direct commits on main)
	@git config core.hooksPath .githooks
	@echo "git core.hooksPath -> .githooks (direct commits on main/master are now blocked)"

.PHONY: test
test: ## run the full test suite with race detector
	CGO_ENABLED=1 $(GO) test -race -count=1 -timeout=60s ./...

.PHONY: test-cover
test-cover: ## run tests + coverage report
	CGO_ENABLED=1 $(GO) test -race -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -20

.PHONY: vet
vet: ## run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## format with gofumpt + goimports (writes in place)
	$(GOFUMPT) -w .
	$(GOIMPORTS) -w -local $(MODULE) .

.PHONY: fmt-check
fmt-check: ## fail when sources are not gofumpt-clean
	@diff=$$($(GOFUMPT) -l .); \
	if [ -n "$$diff" ]; then \
	  echo "gofumpt would rewrite:"; echo "$$diff"; exit 1; \
	fi

.PHONY: lint
lint: ## run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: vuln
vuln: ## scan dependencies + reachable code for known vulnerabilities (govulncheck)
	$(GOVULNCHECK) ./...

.PHONY: tidy
tidy: ## sync go.mod / go.sum
	$(GO) mod tidy

.PHONY: check
check: vet fmt-check lint test ## the pre-commit / pre-push gate

FUZZTIME ?= 5m
COVER_MIN ?= 90

.PHONY: fuzz-smoke
fuzz-smoke: ## run every Fuzz target in ./protocol for 10s each (CI smoke gate)
	@for fn in $$($(GO) test ./protocol -list '^Fuzz' | grep '^Fuzz'); do \
	  echo "== fuzz-smoke: $$fn (10s) =="; \
	  $(GO) test ./protocol -run '^$$' -fuzz "^$$fn$$" -fuzztime=10s; \
	done

.PHONY: fuzz
fuzz: ## run every Fuzz target in ./protocol for FUZZTIME (default 5m)
	@for fn in $$($(GO) test ./protocol -list '^Fuzz' | grep '^Fuzz'); do \
	  echo "== fuzz: $$fn (fuzztime=$(FUZZTIME)) =="; \
	  $(GO) test ./protocol -run '^$$' -fuzz "^$$fn$$" -fuzztime=$(FUZZTIME); \
	done

.PHONY: cover-check
cover-check: ## per-package coverage gate (COVER_MIN percent, default 90); NOT part of `check`
	@status=0; \
	for pkg in $$($(GO) list ./... | cat); do \
	  profile=$$(mktemp); \
	  log=$$(mktemp); \
	  if ! CGO_ENABLED=1 $(GO) test -race -covermode=atomic -coverprofile="$$profile" "$$pkg" >"$$log" 2>&1; then \
	    echo "FAIL $$pkg (test failure)"; cat "$$log"; status=1; rm -f "$$profile" "$$log"; continue; \
	  fi; \
	  total=$$($(GO) tool cover -func="$$profile" | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	  rm -f "$$profile" "$$log"; \
	  if awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN{exit !(t+0>=m+0)}'; then \
	    echo "ok   $$pkg ($$total% >= $(COVER_MIN)%)"; \
	  else \
	    echo "FAIL $$pkg ($$total% < $(COVER_MIN)%)"; status=1; \
	  fi; \
	done; \
	exit $$status

.PHONY: clean
clean: ## remove coverage artefacts
	rm -f coverage.out coverage.* *.coverprofile profile.cov
