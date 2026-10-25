# go-modbus

A minimal, dependency-free, pure-Go **Modbus-TCP client** shared by the
`go-*2mqtt` bridges ([go-mtec2mqtt](https://github.com/SukramJ/go-mtec2mqtt),
[go-keba2mqtt](https://github.com/SukramJ/go-keba2mqtt), …).

It speaks exactly what those devices need and nothing more:

- **FC03** Read Holding Registers and **FC06** Write Single Register, plus
  every exception response, over the MBAP framing (Modbus-TCP).
- One TCP connection, strictly sequential request/response — single-slot
  Modbus stacks (inverter gateways, wallboxes) do not tolerate pipelined
  transactions, so concurrent callers are serialised on the wire.
- **No retry, no reconnect, no pacing.** A broken connection is torn down
  ("poisoned") and the next call reports `ErrNotConnected`; reconnecting,
  backoff and device-specific request pacing belong to the caller, where
  they can be tested without a socket.

Zero third-party dependencies, including in tests.

## Packages

| Package | |
| --- | --- |
| `modbus` (root) | `Client` — `Connect`, `Close`, `IsConnected`, `ReadHoldingRegisters`, `WriteSingleRegister` |
| `protocol` | the MBAP/PDU codec: pure functions over byte slices, fuzzed, and anchored byte-for-byte to pymodbus-generated golden frames |
| `modbustest` | an in-process mock device for tests (`Server`, `Bank`), in the spirit of `net/http/httptest` |

## Usage

```go
c := modbus.New(modbus.Config{
    Host:    "192.168.1.50",
    Port:    502,
    UnitID:  255,
    Timeout: 5 * time.Second,
})
if err := c.Connect(ctx); err != nil {
    return err
}
defer c.Close()

regs, err := c.ReadHoldingRegisters(ctx, 1000, 2) // one UINT32
if exc, ok := errors.AsType[*protocol.ExceptionError](err); ok {
    // the device answered with a Modbus exception
}
value := uint32(regs[0])<<16 | uint32(regs[1])

err = c.WriteSingleRegister(ctx, 5004, 16000) // echo is verified
```

Behaviour worth knowing:

- **Timeouts** — every round-trip is bounded by the tighter of the
  context deadline and `Config.Timeout` (default 5 s).
- **Cancellation** — cancelling the context aborts a transaction that is
  parked in socket I/O immediately (a forced deadline), not after the
  timeout. `Close` does the same for a transaction in flight.
- **Integrity checks** — a mismatched transaction id, unit id, register
  count or FC06 echo is an error; framing errors poison the connection so
  the stream can never desynchronise silently.
- **`IsConnected` never blocks** behind an in-flight transaction — it is
  safe to call from health endpoints.

## Testing against a mock device

```go
bank := modbustest.NewBank().
    SetUint32(1000, 3).   // UINT32 holding register at 1000/1001
    ReadOnly(1000, 1001).
    MaxRead(2)            // a device answering at most two words per request
srv := modbustest.NewServer(t, bank.Handle)

c := modbus.New(modbus.Config{Host: srv.Host(), Port: srv.Port(), UnitID: 255})
// ...
bank.Writes() // every FC06 the device accepted, in order
```

`Server` also takes a hand-written `Handler` and offers fault injection:
`WithDelay`, `WithMutator` (rewrite the response frame — wrong ids,
truncation) and `WithCloseAfterReply`.

## Development

```sh
make setup       # install pinned tooling + git hooks
make check       # vet + fmt-check + lint + test (the pre-push gate)
make fuzz-smoke  # 10 s per protocol fuzz target
```

`protocol/testdata/` holds golden MBAP frames generated and cross-checked
with pymodbus (`generate_vectors.py`, `cross_check.py`).

## License

MIT — see [LICENSE](LICENSE).
