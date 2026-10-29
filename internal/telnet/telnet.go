// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package telnet strips a small part of RFC 854 off a byte stream.
// The session underneath is the same carriage-return text as an AX.25
// connection. This station does not echo, and it asks the peer not to
// wait for a go-ahead. A data byte of 255 is sent as two IAC bytes.
// A line feed or NUL after a carriage return is left in the stream.
// The session skips that byte on a line. A counted bulletin body keeps it.
package telnet

import (
	"bufio"
	"io"
	"sync"
	"sync/atomic"
)

const (
	iac     = 255
	dont    = 254
	do      = 253
	wont    = 252
	will    = 251
	sb      = 250
	se      = 240
	optEcho = 1
	optSGA  = 3
)

// Wrap returns a stream with telnet commands removed. Greeting bytes are
// written on the first read or write. One reader at a time is enough.
func Wrap(raw io.ReadWriteCloser) io.ReadWriteCloser {
	return &conn{
		raw:  raw,
		br:   bufio.NewReader(raw),
		sent: map[byte]byte{},
	}
}

type conn struct {
	raw     io.ReadWriteCloser
	br      *bufio.Reader
	once    sync.Once
	mu      sync.Mutex
	willSGA bool
	doSGA   bool
	sent    map[byte]byte
	pend    atomic.Value // error, first failure only
}

func (c *conn) greet() {
	c.once.Do(func() {
		c.willSGA = true
		c.doSGA = true
		c.sent[optEcho] = wont
		c.setPend(writeFull(c.raw, []byte{
			iac, will, optSGA,
			iac, wont, optEcho,
			iac, do, optSGA,
		}))
	})
}

func (c *conn) getPend() error {
	v := c.pend.Load()
	if v == nil {
		return nil
	}
	return v.(error)
}

func (c *conn) setPend(err error) {
	if err == nil {
		return
	}
	c.pend.CompareAndSwap(nil, err)
}

func (c *conn) Read(p []byte) (int, error) {
	c.greet()
	if len(p) == 0 {
		return 0, nil
	}
	n := 0
	for n < len(p) {
		b, err := c.next()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		p[n] = b
		n++
		if c.br.Buffered() == 0 {
			break
		}
	}
	return n, nil
}

func (c *conn) next() (byte, error) {
	for {
		if err := c.getPend(); err != nil && c.br.Buffered() == 0 {
			return 0, err
		}
		b, err := c.br.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != iac {
			return b, nil
		}
		cmd, err := c.br.ReadByte()
		if err != nil {
			return 0, err
		}
		switch cmd {
		case iac:
			return iac, nil
		case will, wont, do, dont:
			opt, err := c.br.ReadByte()
			if err != nil {
				return 0, err
			}
			c.reply(cmd, opt)
		case sb:
			if err := c.skipSB(); err != nil {
				return 0, err
			}
		}
	}
}

func (c *conn) skipSB() error {
	for {
		b, err := c.br.ReadByte()
		if err != nil {
			return err
		}
		if b != iac {
			continue
		}
		cmd, err := c.br.ReadByte()
		if err != nil {
			return err
		}
		if cmd == se {
			return nil
		}
	}
}

func (c *conn) reply(cmd, opt byte) {
	var out []byte
	switch cmd {
	case do:
		if opt == optSGA {
			if !c.willSGA {
				c.willSGA = true
				out = []byte{iac, will, optSGA}
			}
			break
		}
		if c.mark(opt, wont) {
			out = []byte{iac, wont, opt}
		}
	case dont:
		if opt == optSGA && c.willSGA {
			c.willSGA = false
			out = []byte{iac, wont, optSGA}
		}
	case will:
		if opt == optSGA {
			if !c.doSGA {
				c.doSGA = true
				out = []byte{iac, do, optSGA}
			}
			break
		}
		if c.mark(opt, dont) {
			out = []byte{iac, dont, opt}
		}
	case wont:
		if opt == optSGA && c.doSGA {
			c.doSGA = false
			out = []byte{iac, dont, optSGA}
		}
	}
	if len(out) == 0 {
		return
	}
	c.mu.Lock()
	err := writeFull(c.raw, out)
	c.mu.Unlock()
	c.setPend(err)
}

func (c *conn) mark(opt, cmd byte) bool {
	if c.sent[opt] == cmd {
		return false
	}
	c.sent[opt] = cmd
	return true
}

func (c *conn) Write(p []byte) (int, error) {
	c.greet()
	if err := c.getPend(); err != nil {
		return 0, err
	}
	buf := make([]byte, 0, len(p))
	for _, b := range p {
		if b == iac {
			buf = append(buf, iac, iac)
			continue
		}
		buf = append(buf, b)
	}
	c.mu.Lock()
	err := writeFull(c.raw, buf)
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *conn) Close() error {
	return c.raw.Close()
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
