// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package modbustest_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-modbus"
	"github.com/SukramJ/go-modbus/modbustest"
	"github.com/SukramJ/go-modbus/protocol"
)

func TestServerAddrAndDelay(t *testing.T) {
	t.Parallel()
	srv := modbustest.NewServer(t, func([]byte) ([]byte, *protocol.ExceptionError) {
		return modbustest.FC03([]uint16{42}), nil
	}).WithDelay(100 * time.Millisecond)
	if srv.Addr() != net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.Port())) {
		t.Fatalf("Addr = %q", srv.Addr())
	}
	c := dial(t, srv)
	start := time.Now()
	regs, err := c.ReadHoldingRegisters(context.Background(), 0, 1)
	if err != nil || regs[0] != 42 {
		t.Fatalf("read = %v, %v", regs, err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("WithDelay did not hold the response back")
	}
}

func TestServerCloseAfterReply(t *testing.T) {
	t.Parallel()
	srv := modbustest.NewServer(t, func([]byte) ([]byte, *protocol.ExceptionError) {
		return modbustest.FC03([]uint16{1}), nil
	}).WithCloseAfterReply()
	c := dial(t, srv)
	ctx := context.Background()
	if _, err := c.ReadHoldingRegisters(ctx, 0, 1); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if _, err := c.ReadHoldingRegisters(ctx, 0, 1); err == nil {
		t.Fatal("read on a connection the server closed succeeded")
	}
	if c.IsConnected() {
		t.Fatal("client still connected after the peer closed")
	}
	srv.Close()
	srv.Close() // idempotent
}

func TestServerMutatorAndUnknownFunction(t *testing.T) {
	t.Parallel()
	bank := modbustest.NewBank().Set(0, 1)
	srv := modbustest.NewServer(t, bank.Handle).WithMutator(func(frame []byte) []byte {
		frame[6] = 0x01 // wrong unit id
		return frame
	})
	c := dial(t, srv)
	_, err := c.ReadHoldingRegisters(context.Background(), 0, 1)
	if !errors.Is(err, modbus.ErrUnitMismatch) {
		t.Fatalf("err = %v, want ErrUnitMismatch", err)
	}

	pdu, exc := bank.Handle([]byte{0x10, 0, 0, 0, 1})
	if pdu != nil || exc == nil || exc.ExceptionCode != protocol.ExceptionIllegalFunction {
		t.Fatalf("FC16 = %v/%v, want ILLEGAL FUNCTION", pdu, exc)
	}
	if _, exc := bank.Handle([]byte{0x03}); exc == nil {
		t.Fatal("short request accepted")
	}
	if v, ok := bank.Get(1); ok {
		t.Fatalf("undefined register reported %d", v)
	}
}
