// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package modbustest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-modbus"
	"github.com/SukramJ/go-modbus/modbustest"
	"github.com/SukramJ/go-modbus/protocol"
)

func dial(t *testing.T, srv *modbustest.Server) *modbus.Client {
	t.Helper()
	c := modbus.New(modbus.Config{Host: srv.Host(), Port: srv.Port(), UnitID: 255, Timeout: time.Second})
	if err := c.Connect(t.Context()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestBankServesUint32AndRecordsWrites(t *testing.T) {
	t.Parallel()
	var hooked []uint16
	bank := modbustest.NewBank().
		SetUint32(1000, 0x0001_0003).
		ReadOnly(1000, 1001).
		OnWrite(func(a, _ uint16) { hooked = append(hooked, a) })
	srv := modbustest.NewServer(t, bank.Handle)
	c := dial(t, srv)
	ctx := context.Background()

	regs, err := c.ReadHoldingRegisters(ctx, 1000, 2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !slices.Equal(regs, []uint16{1, 3}) {
		t.Fatalf("read = %v, want [1 3]", regs)
	}
	if err := c.WriteSingleRegister(ctx, 5004, 16000); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := bank.Writes(); !slices.Equal(got, []modbustest.Write{{Address: 5004, Value: 16000}}) {
		t.Fatalf("writes = %v", got)
	}
	if v, ok := bank.Get(5004); !ok || v != 16000 {
		t.Fatalf("bank 5004 = %d/%v, want 16000", v, ok)
	}
	if !slices.Equal(hooked, []uint16{5004}) {
		t.Fatalf("hook saw %v", hooked)
	}
	if srv.Requests() != 2 {
		t.Fatalf("requests = %d, want 2", srv.Requests())
	}
}

func TestBankRefusals(t *testing.T) {
	t.Parallel()
	bank := modbustest.NewBank().SetUint32(1000, 7).ReadOnly(1000).MaxRead(2)
	srv := modbustest.NewServer(t, bank.Handle)
	c := dial(t, srv)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
		code byte
	}{
		{"unknown address", func() error { _, err := c.ReadHoldingRegisters(ctx, 2000, 2); return err }, protocol.ExceptionIllegalDataAddress},
		{"over max read", func() error { _, err := c.ReadHoldingRegisters(ctx, 1000, 3); return err }, protocol.ExceptionIllegalDataValue},
		{"read-only write", func() error { return c.WriteSingleRegister(ctx, 1000, 1) }, protocol.ExceptionIllegalDataAddress},
	}
	for _, tc := range cases {
		err := tc.call()
		exc, ok := errors.AsType[*protocol.ExceptionError](err)
		if !ok {
			t.Fatalf("%s: err = %v, want an exception", tc.name, err)
		}
		if exc.ExceptionCode != tc.code {
			t.Errorf("%s: code = %#x, want %#x", tc.name, exc.ExceptionCode, tc.code)
		}
	}
	if len(bank.Writes()) != 0 {
		t.Fatalf("refused write was recorded: %v", bank.Writes())
	}
}
