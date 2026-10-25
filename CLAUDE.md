# CLAUDE.md — AI Assistant Guide for go-modbus

## Project Overview

**go-modbus** is a minimal, dependency-free, pure-Go **Modbus-TCP client
library**: an MBAP/PDU codec for FC03 (Read Holding Registers) and FC06
(Write Single Register), a single-connection sequential client, and an
exported mock device for tests. It is a **library module**, not a daemon —
no `main` package, no binaries, no Docker image, no config file.

The code was extracted from go-mtec2mqtt's `internal/modbus` when
go-keba2mqtt needed the same transport. The `go-*2mqtt` bridges import it
as a regular `go.mod` dependency.

## Key Characteristics

- **Language**: Go 1.27+ (see `go.mod`).
- **Module path**: `github.com/SukramJ/go-modbus`.
- **License: MIT.** Every Go source file starts with:
  ```go
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 SukramJ
  ```
- **Zero third-party dependencies** — deliberately, including in tests.
  `go.mod` has no `require` block; keep it that way.
- **Scope is narrow on purpose.** Only the function codes a consumer
  actually speaks. Add a function code when a bridge needs it, with
  golden frames and a fuzz target, not speculatively.
- **No policy in the transport.** No retry, reconnect, backoff or request
  pacing: the client poisons a broken connection and reports
  `ErrNotConnected`; consumers own resilience and device-specific pacing
  (e.g. the KEBA P40's 0.5 s read / 5 s write intervals live in
  go-keba2mqtt).

## Repository Structure

```
client.go               package modbus: Config, Client (Connect/Close/IsConnected/ReadHoldingRegisters/WriteSingleRegister), deadline hook
client_test.go          client behaviour against modbustest.Server
scriptedconn_test.go    in-memory net.Conn that pins the exact instant a read completes (cancel-hook races)
protocol/mbap.go        MBAP header + FC03/FC06 PDU encode/decode, ExceptionError
protocol/*_test.go      golden-frame tests and fuzz targets
protocol/testdata/      pymodbus-generated golden frames + generate_vectors.py / cross_check.py
modbustest/server.go    exported mock device: Server, Handler, FC03/FC06/Exception helpers
modbustest/bank.go      Bank: simulated holding-register file (read-only addresses, read cap, write log, write hook)
.github/workflows/      ci.yml (lint, test matrix, fuzz-smoke), codeql.yml, release-on-tag.yml, dependabot-auto-merge.yml
.githooks/              pre-commit hook blocking direct commits on main/master
```

## Development Commands

All defined in the `Makefile` (`make help` lists them):

```sh
make setup       # install pinned gofumpt/golangci-lint/govulncheck + goimports, git hooks
make test        # go test -race -count=1 ./...
make check       # vet + fmt-check + lint + test — the pre-commit/pre-push gate
make fuzz-smoke  # 10 s per Fuzz target in ./protocol (CI gate)
make fuzz        # FUZZTIME (default 5m) per target
make cover-check # per-package coverage gate (COVER_MIN, default 90)
make vuln        # govulncheck
```

## Code Conventions

- **Formatting**: `gofumpt` + `goimports -local github.com/SukramJ/go-modbus`.
- **Logging**: `log/slog` only; the client logs connect/disconnect at info.
- **Errors**: sentinel errors are wrapped, never compared with `==`;
  device exceptions surface as `*protocol.ExceptionError` (`errors.AsType`).
- **Wire correctness is anchored externally**: change the codec only
  together with golden frames regenerated/cross-checked against pymodbus.
- **Git hooks**: `make hooks` blocks direct commits on `main`/`master`.
- **Commit style**: Conventional Commits with a scope
  (`feat(client): …`, `fix(protocol): …`, `chore(deps): …`).
- **Releases**: add a `## [x.y.z] - date` section to `CHANGELOG.md`, then
  tag `vx.y.z`; `release-on-tag.yml` publishes that section as the
  GitHub release (it fails if the section is missing). Consumers pin the
  exact version.
- **API stability**: v0.x. Exported names are used by every bridge; a
  breaking change means updating go-mtec2mqtt and go-keba2mqtt in step.
