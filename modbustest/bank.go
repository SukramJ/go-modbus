// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package modbustest

import (
	"encoding/binary"
	"sync"

	"github.com/SukramJ/go-modbus/protocol"
)

// Bank is a simulated holding-register file: FC03 reads are answered
// from it, FC06 writes are stored into it and recorded. Addresses that
// were never set answer ILLEGAL DATA ADDRESS, the way a real device
// answers a register it does not implement.
//
// It is safe for concurrent use; hand [Bank.Handle] to [NewServer].
type Bank struct {
	mu     sync.Mutex
	regs   map[uint16]uint16
	writes []Write
	// readOnly addresses refuse FC06 with ILLEGAL DATA ADDRESS.
	readOnly map[uint16]bool
	// maxRead caps the FC03 quantity a single request may ask for; zero
	// means the protocol limit. Devices such as the KEBA P40 answer at
	// most two words per request.
	maxRead int
	// onWrite, when set, runs (outside the lock) after a write was
	// stored, so a test can simulate a device reacting to a command.
	onWrite func(address, value uint16)
}

// Write is one FC06 the bank accepted, in arrival order.
type Write struct {
	Address uint16
	Value   uint16
}

// NewBank returns an empty bank.
func NewBank() *Bank {
	return &Bank{regs: map[uint16]uint16{}, readOnly: map[uint16]bool{}}
}

// Set stores one 16-bit register.
func (b *Bank) Set(address, value uint16) *Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.regs[address] = value
	return b
}

// SetUint32 stores v big-endian across address (high word) and
// address+1 (low word) — the layout of a UINT32 holding register.
func (b *Bank) SetUint32(address uint16, v uint32) *Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.regs[address] = uint16(v >> 16)
	b.regs[address+1] = uint16(v) //nolint:gosec // low word, truncation intended
	return b
}

// Get returns a register's current value and whether it is defined.
func (b *Bank) Get(address uint16) (uint16, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.regs[address]
	return v, ok
}

// ReadOnly marks addresses that refuse FC06.
func (b *Bank) ReadOnly(addresses ...uint16) *Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, a := range addresses {
		b.readOnly[a] = true
	}
	return b
}

// MaxRead caps the FC03 quantity per request; a larger request answers
// ILLEGAL DATA VALUE.
func (b *Bank) MaxRead(n int) *Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maxRead = n
	return b
}

// OnWrite installs a hook that runs after every accepted write.
func (b *Bank) OnWrite(fn func(address, value uint16)) *Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onWrite = fn
	return b
}

// Writes returns a copy of every accepted FC06, in arrival order.
func (b *Bank) Writes() []Write {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Write(nil), b.writes...)
}

// Handle implements [Handler].
func (b *Bank) Handle(req []byte) ([]byte, *protocol.ExceptionError) {
	if len(req) != 5 {
		return nil, Exception(protocol.ExceptionIllegalDataValue)
	}
	address := binary.BigEndian.Uint16(req[1:3])
	arg := binary.BigEndian.Uint16(req[3:5])
	switch req[0] {
	case protocol.FCReadHoldingRegisters:
		return b.read(address, arg)
	case protocol.FCWriteSingleRegister:
		return b.write(address, arg)
	default:
		return nil, Exception(protocol.ExceptionIllegalFunction)
	}
}

func (b *Bank) read(address, count uint16) ([]byte, *protocol.ExceptionError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if count == 0 || (b.maxRead > 0 && int(count) > b.maxRead) {
		return nil, Exception(protocol.ExceptionIllegalDataValue)
	}
	values := make([]uint16, count)
	for i := range values {
		v, ok := b.regs[address+uint16(i)] //nolint:gosec // i < count <= 125
		if !ok {
			return nil, Exception(protocol.ExceptionIllegalDataAddress)
		}
		values[i] = v
	}
	return FC03(values), nil
}

func (b *Bank) write(address, value uint16) ([]byte, *protocol.ExceptionError) {
	b.mu.Lock()
	if b.readOnly[address] {
		b.mu.Unlock()
		return nil, Exception(protocol.ExceptionIllegalDataAddress)
	}
	b.regs[address] = value
	b.writes = append(b.writes, Write{Address: address, Value: value})
	hook := b.onWrite
	b.mu.Unlock()
	if hook != nil {
		hook(address, value)
	}
	return FC06(address, value), nil
}
