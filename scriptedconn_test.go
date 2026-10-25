// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package modbus

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/SukramJ/go-modbus/modbustest"
	"github.com/SukramJ/go-modbus/protocol"
)

// --- scriptedConn -----------------------------------------------------------

// scriptedConn is an in-memory net.Conn stand-in that answers every FC03
// request from Client.do and, unlike a real socket, lets a test pin the
// exact instant at which a response is "fully read". That is what makes
// the stale-cancel-hook regression reproducible: the cancellation has to
// land in the few microseconds between the last successful read and
// do()'s return.
//
// It models the one socket behaviour the cancel hook relies on: a
// deadline in the past aborts pending and subsequent I/O with
// os.ErrDeadlineExceeded, while a fresh future deadline re-arms it.
type scriptedConn struct {
	mu        sync.Mutex
	buf       []byte
	expired   bool
	expiredCh chan struct{}
	// forced counts SetDeadline calls with a non-future deadline, i.e.
	// exactly the hook's "abort now" signal.
	forced int
	// delay holds every Read back before it serves bytes — widens the
	// window in which a stray deadline can kill an in-flight read.
	delay time.Duration
	// afterFinalRead runs right after the read that drains the buffer,
	// i.e. once the client has consumed the whole response frame.
	afterFinalRead func()
	closed         bool
}

func newScriptedConn() *scriptedConn {
	return &scriptedConn{expiredCh: make(chan struct{})}
}

func (s *scriptedConn) setDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

func (s *scriptedConn) setAfterFinalRead(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterFinalRead = fn
}

// forcedDeadlines reports how often something forced an immediate
// deadline on this connection.
func (s *scriptedConn) forcedDeadlines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forced
}

func (s *scriptedConn) Read(p []byte) (int, error) {
	s.mu.Lock()
	expired, ch, delay := s.expired, s.expiredCh, s.delay
	s.mu.Unlock()
	if expired {
		return 0, os.ErrDeadlineExceeded
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ch:
			return 0, os.ErrDeadlineExceeded
		}
	}

	s.mu.Lock()
	switch {
	case s.expired:
		s.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	case s.closed, len(s.buf) == 0:
		s.mu.Unlock()
		return 0, io.EOF
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	hook := s.afterFinalRead
	drained := len(s.buf) == 0
	s.mu.Unlock()

	if drained && hook != nil {
		hook()
	}
	return n, nil
}

// Write decodes the outgoing MBAP frame and queues a matching FC03
// reply, so the client's transaction-id and unit-id checks pass.
func (s *scriptedConn) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.expired {
		s.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	}
	if s.closed {
		s.mu.Unlock()
		return 0, net.ErrClosed
	}
	s.mu.Unlock()

	tid := binary.BigEndian.Uint16(p[0:2])
	unit := p[6]
	count := binary.BigEndian.Uint16(p[10:12])
	pdu := modbustest.FC03(make([]uint16, count))
	frame := make([]byte, protocol.HeaderLen+len(pdu))
	binary.BigEndian.PutUint16(frame[0:2], tid)
	binary.BigEndian.PutUint16(frame[4:6], uint16(len(pdu)+1))
	frame[6] = unit
	copy(frame[protocol.HeaderLen:], pdu)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, frame...)
	return len(p), nil
}

func (s *scriptedConn) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	if t.IsZero() || t.After(time.Now()) {
		if s.expired {
			s.expired = false
			s.expiredCh = make(chan struct{})
		}
		return nil
	}
	s.forced++
	if !s.expired {
		s.expired = true
		close(s.expiredCh)
	}
	return nil
}

func (s *scriptedConn) SetReadDeadline(t time.Time) error  { return s.SetDeadline(t) }
func (s *scriptedConn) SetWriteDeadline(t time.Time) error { return s.SetDeadline(t) }

func (s *scriptedConn) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *scriptedConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

func (s *scriptedConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}
}
