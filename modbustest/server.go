// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

// Package modbustest provides an in-process Modbus-TCP responder for
// tests, in the spirit of net/http/httptest.
//
// It is the mock device the go-modbus client tests run against, exported
// so that consumers (the go-*2mqtt bridges) can drive their own device
// layers against the same server instead of each keeping a private copy.
// It is not safe for production use: it trusts every frame it receives
// and implements only what the client in the parent package speaks.
package modbustest

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-modbus/protocol"
)

// Handler decides how the server replies to a single decoded request
// PDU (function code first). It returns the response PDU (function code
// included) or a non-nil *protocol.ExceptionError. If both are nil the
// connection is closed without a reply — useful for simulating a peer
// that drops mid-request.
type Handler func(req []byte) (pdu []byte, exc *protocol.ExceptionError)

// Server is a minimal Modbus-TCP responder bound to 127.0.0.1 on a free
// port. Construct with [NewServer]; it is closed automatically when the
// test that created it ends.
type Server struct {
	listener net.Listener
	addr     *net.TCPAddr
	handler  Handler

	// cfgMu guards the knobs below, which a test may change while the
	// server is already serving.
	cfgMu sync.Mutex
	delay time.Duration
	// mutate, when set, rewrites the fully encoded response frame just
	// before it is written — used to inject wrong transaction-ids,
	// unit-ids or truncated frames. Returning a shorter slice combined
	// with closeAfterReply simulates a peer dying mid-response.
	mutate          func(frame []byte) []byte
	closeAfterReply bool

	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
	count  int
}

// NewServer starts a server answering every request through h. The
// server is closed via tb.Cleanup.
func NewServer(tb testing.TB, h Handler) *Server {
	tb.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("modbustest: listen: %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		tb.Fatalf("modbustest: listener address is %T, not *net.TCPAddr", l.Addr())
	}
	s := &Server{
		listener: l,
		addr:     addr,
		handler:  h,
	}
	tb.Cleanup(s.Close)
	s.wg.Add(1)
	go s.acceptLoop()
	return s
}

// Host returns the address the server listens on ("127.0.0.1").
func (s *Server) Host() string { return s.addr.IP.String() }

// Port returns the TCP port the server listens on.
func (s *Server) Port() int { return s.addr.Port }

// Addr returns "host:port".
func (s *Server) Addr() string { return net.JoinHostPort(s.Host(), strconv.Itoa(s.Port())) }

// Requests returns how many request frames the server has decoded so
// far, including ones answered with an exception or dropped.
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// WithDelay holds back each response by d — used to provoke timeouts.
func (s *Server) WithDelay(d time.Duration) *Server {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.delay = d
	return s
}

// WithMutator installs a response-frame rewriter (see the mutate field).
func (s *Server) WithMutator(fn func(frame []byte) []byte) *Server {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.mutate = fn
	return s
}

// WithCloseAfterReply drops the connection right after each response —
// combined with a truncating mutator this leaves the client waiting for
// bytes that never arrive.
func (s *Server) WithCloseAfterReply() *Server {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.closeAfterReply = true
	return s
}

// Close stops the listener and waits for every connection to end. Safe
// to call more than once.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.listener.Close()
	s.wg.Wait()
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		s.wg.Add(1)
		go s.serve(conn)
	}
}

func (s *Server) knobs() (delay time.Duration, mutate func([]byte) []byte, closeAfter bool) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.delay, s.mutate, s.closeAfterReply
}

func (s *Server) serve(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()
	for {
		header := make([]byte, protocol.HeaderLen)
		if _, err := io.ReadFull(conn, header); err != nil {
			// EOF/closed on shutdown or a transient read error —
			// either way, drop the connection.
			return
		}
		h, err := protocol.DecodeHeader(header)
		if err != nil {
			return
		}
		req := make([]byte, h.Length())
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		s.mu.Lock()
		s.count++
		s.mu.Unlock()
		delay, mutate, closeAfter := s.knobs()
		if delay > 0 {
			time.Sleep(delay)
		}
		pdu, exc := s.handler(req)
		if pdu == nil && exc == nil {
			// Handler signalled "drop the connection mid-request".
			return
		}
		respPDU := pdu
		if exc != nil {
			respPDU = []byte{req[0] | 0x80, exc.ExceptionCode}
		}
		out, err := protocol.EncodeFrame(h.TransactionID, h.UnitID, respPDU)
		if err != nil {
			return
		}
		if mutate != nil {
			out = mutate(out)
		}
		if _, err := conn.Write(out); err != nil {
			return
		}
		if closeAfter {
			return
		}
	}
}

// FC03 builds a normal FC03 response PDU carrying values. Callers keep
// len(values) within the 125-register FC03 limit.
func FC03(values []uint16) []byte {
	out := make([]byte, 2+len(values)*2)
	out[0] = protocol.FCReadHoldingRegisters
	out[1] = byte(len(values) * 2) //nolint:gosec // bounded by the FC03 limit, see doc comment
	for i, v := range values {
		binary.BigEndian.PutUint16(out[2+i*2:], v)
	}
	return out
}

// FC06 builds a normal FC06 echo response PDU.
func FC06(address, value uint16) []byte {
	return protocol.EncodeWriteSingleRegister(address, value)
}

// Exception returns the exception a [Handler] answers with for code.
func Exception(code byte) *protocol.ExceptionError {
	return &protocol.ExceptionError{ExceptionCode: code}
}
