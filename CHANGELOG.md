# Changelog

All notable changes to this project are documented in this file. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.1.0] - 2026-10-25

### Added

- First release, extracted from go-mtec2mqtt's `internal/modbus` so the
  `go-*2mqtt` bridges share one Modbus-TCP implementation.
- `modbus.Client`: FC03 Read Holding Registers and FC06 Write Single
  Register over MBAP, with context- and timeout-bounded round-trips,
  immediate abort of in-flight I/O on cancellation and on `Close`,
  transaction-id / unit-id / quantity / echo verification, and a
  lock-free `IsConnected`.
- `protocol`: the pure MBAP/PDU codec, its fuzz targets and the
  pymodbus-generated golden frames it is checked against.
- `modbustest`: the mock device the client tests use, exported —
  `Server` (handler, delay, frame mutator, close-after-reply, request
  count) and `Bank`, a simulated holding-register file with read-only
  addresses, a per-request read cap and a write log.
