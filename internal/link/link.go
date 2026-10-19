// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package link is the AX/UDP port. Each datagram is one AX.25 frame with
// its frame check sequence and without HDLC flags, the encapsulation in
// RFC 1226, carried on UDP. Port 10093 is the port other nodes expect.
// The bytes are readable text.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/settings"
)

const (
	maxDatagram    = 1024
	maxGuestsPerIP = 4
	guestHold      = 2 * time.Second
	termLineWait   = 5 * time.Second
	peerReserveMax = 8
)

// guestRoute is the return address of a station that is not in the peer list.
type guestRoute struct {
	addr   *net.UDPAddr
	active bool
	seen   time.Time
}

// Config is one AX/UDP station.
type Config struct {
	Bind         string
	Local        ax25.Address
	Peers        string
	TerminalPath string
	TerminalMode os.FileMode
	Paclen       int
	Window       int
	T1           time.Duration
	N2           int
	Lookup       settings.LookupIP
	Handler      func(ax25.Address, *ax25.Stream)
}

// Node is a running AX/UDP station.
type Node struct {
	local    ax25.Address
	pc       *net.UDPConn
	sw       *ax25.Switch
	lookup   settings.LookupIP
	dialFor  time.Duration
	termPath string
	term     net.Listener
	quit     chan struct{}
	once     sync.Once

	lineWait time.Duration

	mu       sync.Mutex
	peers    map[string]*net.UDPAddr
	peerPort map[string]int
	guests   map[string]*guestRoute
	drop     func(ax25.Frame) bool
	terms    int
}

// Listen binds the UDP port and, when TerminalPath is set, the local
// terminal socket. Peers is the same text as the link.peers setting.
func Listen(cfg Config) (*Node, error) {
	if cfg.Local.Call == "" {
		return nil, errors.New("station callsign is empty")
	}
	if strings.TrimSpace(cfg.Bind) == "" {
		return nil, errors.New("link.listen is empty")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", cfg.Bind)
	if err != nil {
		return nil, err
	}
	pc, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	mode := cfg.TerminalMode.Perm()
	if cfg.TerminalPath != "" && mode == 0 {
		mode = 0o600
	}
	if cfg.TerminalPath != "" && mode != 0o600 && mode != 0o660 {
		pc.Close()
		return nil, errors.New("socket mode must be 0600 or 0660")
	}
	t1 := cfg.T1
	if t1 <= 0 {
		t1 = ax25.DefaultT1
	}
	n2 := cfg.N2
	if n2 < 1 {
		n2 = ax25.DefaultN2
	}
	n := &Node{
		local:    cfg.Local,
		pc:       pc,
		lookup:   cfg.Lookup,
		dialFor:  t1 * time.Duration(n2+2),
		termPath: cfg.TerminalPath,
		lineWait: termLineWait,
		quit:     make(chan struct{}),
		peers:    map[string]*net.UDPAddr{},
		peerPort: map[string]int{},
		guests:   map[string]*guestRoute{},
	}
	if err := n.SetPeers(cfg.Peers); err != nil {
		pc.Close()
		return nil, err
	}
	accept := cfg.Handler
	if accept == nil {
		accept = func(_ ax25.Address, st *ax25.Stream) { servePrompt(cfg.Local, st) }
	}
	n.sw = ax25.NewSwitch(ax25.Config{
		Local:  cfg.Local,
		Paclen: cfg.Paclen,
		Window: cfg.Window,
		N2:     cfg.N2,
		T1:     cfg.T1,
		Send:   n.transmit,
		Accept: accept,
		Begin:  n.beginSession,
		End:    n.endSession,
	})
	go n.readLoop()
	if cfg.TerminalPath != "" {
		ln, err := listenUnix(cfg.TerminalPath, mode)
		if err != nil {
			n.Close()
			return nil, err
		}
		n.term = ln
		go n.acceptTerm()
	}
	return n, nil
}

// Local returns this station's AX.25 address.
func (n *Node) Local() ax25.Address { return n.local }

// LocalAddr returns the bound UDP address.
func (n *Node) LocalAddr() *net.UDPAddr {
	return n.pc.LocalAddr().(*net.UDPAddr)
}

// Terminal returns the terminal socket path. It is empty when the node
// has no terminal socket.
func (n *Node) Terminal() string { return n.termPath }

// SetPeers replaces the stations this node can call. The text is the
// link.peers value. Hostnames are resolved now.
func (n *Node) SetPeers(text string) error {
	list, err := settings.ParsePeers(text)
	if err != nil {
		return err
	}
	next := make(map[string]*net.UDPAddr, len(list))
	for _, peer := range list {
		addr, err := n.resolve(peer.Host, peer.Port)
		if err != nil {
			return err
		}
		next[peer.Call] = addr
	}
	n.mu.Lock()
	n.peers = next
	n.peerPort = map[string]int{}
	for call := range next {
		delete(n.guests, call)
	}
	n.mu.Unlock()
	return nil
}

// Dial connects to a listed station and returns the byte stream.
func (n *Node) Dial(ctx context.Context, call string) (*ax25.Stream, error) {
	addr, err := ax25.Parse(call)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	_, ok := n.peers[addr.String()]
	n.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%s is not listed", addr)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sw.Dial(ctx, addr)
}

// Close stops the UDP port, the terminal socket, and open sessions.
func (n *Node) Close() error {
	n.once.Do(func() {
		close(n.quit)
		if n.pc != nil {
			_ = n.pc.Close()
		}
		if n.term != nil {
			_ = n.term.Close()
			_ = os.Remove(n.termPath)
		}
		if n.sw != nil {
			n.sw.Close()
		}
	})
	return nil
}

func (n *Node) readLoop() {
	buf := make([]byte, maxDatagram+1)
	for {
		count, from, err := n.pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		// A full buffer means the datagram was longer than an AX.25 frame we accept.
		if count == 0 || count > maxDatagram || from == nil {
			continue
		}
		frame, err := ax25.Decode(buf[:count])
		if err != nil || !frame.Dest.Equal(n.local) || len(frame.Digis) > 0 || ax25.IsUI(frame.Control) {
			continue
		}
		if !n.allow(frame.Src, from) {
			continue
		}
		n.sw.Inject(frame)
	}
}

func (n *Node) transmit(remote ax25.Address, frame ax25.Frame) {
	n.mu.Lock()
	drop := n.drop
	addr := n.lookupAddr(remote)
	n.mu.Unlock()
	if drop != nil && drop(frame) {
		return
	}
	if addr == nil {
		return
	}
	_, _ = n.pc.WriteToUDP(frame.Encode(), addr)
}

func (n *Node) lookupAddr(remote ax25.Address) *net.UDPAddr {
	key := remote.String()
	if peer, ok := n.peers[key]; ok {
		port := peer.Port
		if learned, ok := n.peerPort[key]; ok {
			port = learned
		}
		return &net.UDPAddr{IP: peer.IP, Port: port}
	}
	if guest, ok := n.guests[key]; ok {
		return &net.UDPAddr{IP: guest.addr.IP, Port: guest.addr.Port}
	}
	return nil
}

func (n *Node) allow(src ax25.Address, from *net.UDPAddr) bool {
	ip := canonicalIP(from.IP)
	if from.Port < 1 || ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return false
	}
	key := src.String()
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sweepGuestsLocked(now)
	if peer, ok := n.peers[key]; ok {
		if !peer.IP.Equal(ip) {
			return false
		}
		if from.Port != peer.Port {
			n.peerPort[key] = from.Port
		} else {
			delete(n.peerPort, key)
		}
		return true
	}
	if guest, ok := n.guests[key]; ok {
		if !guest.addr.IP.Equal(ip) {
			return false
		}
		guest.addr.Port = from.Port
		guest.seen = now
		return true
	}
	if len(n.guests) >= n.guestLimitLocked() || n.guestsFromLocked(ip) >= maxGuestsPerIP {
		return false
	}
	n.guests[key] = &guestRoute{
		addr: &net.UDPAddr{IP: append(net.IP(nil), ip...), Port: from.Port},
		seen: now,
	}
	return true
}

func (n *Node) guestLimitLocked() int {
	reserve := len(n.peers)
	if reserve > peerReserveMax {
		reserve = peerReserveMax
	}
	limit := ax25.MaxSessions - reserve
	if limit < 1 {
		limit = 1
	}
	return limit
}

func (n *Node) guestsFromLocked(ip net.IP) int {
	count := 0
	for _, guest := range n.guests {
		if guest.addr.IP.Equal(ip) {
			count++
		}
	}
	return count
}

func (n *Node) sweepGuestsLocked(now time.Time) {
	for key, guest := range n.guests {
		if guest.active || now.Sub(guest.seen) < guestHold {
			continue
		}
		delete(n.guests, key)
	}
}

func (n *Node) beginSession(remote ax25.Address) {
	n.mu.Lock()
	defer n.mu.Unlock()
	guest, ok := n.guests[remote.String()]
	if !ok || guest.active {
		return
	}
	guest.active = true
}

func (n *Node) endSession(remote ax25.Address) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := remote.String()
	if _, ok := n.guests[key]; !ok {
		return
	}
	delete(n.guests, key)
}

func canonicalIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return append(net.IP(nil), v4...)
	}
	return append(net.IP(nil), ip...)
}

func (n *Node) resolve(host string, port int) (*net.UDPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	lookup := n.lookup
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	seen := map[string]net.IP{}
	var chosen net.IP
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		if _, ok := seen[ip.String()]; ok {
			continue
		}
		seen[ip.String()] = ip
		chosen = ip
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("cannot resolve %s", host)
	}
	if len(seen) > 1 {
		return nil, fmt.Errorf("%s resolves to more than one address; set one IP for that peer", host)
	}
	return &net.UDPAddr{IP: chosen, Port: port}, nil
}

func (n *Node) acceptTerm() {
	for {
		conn, err := n.term.Accept()
		if err != nil {
			return
		}
		n.mu.Lock()
		if n.terms >= 8 {
			n.mu.Unlock()
			fmt.Fprintf(conn, "ERR too many terminals\n")
			conn.Close()
			continue
		}
		n.terms++
		n.mu.Unlock()
		go func() {
			n.serveTerm(conn)
			n.mu.Lock()
			n.terms--
			n.mu.Unlock()
		}()
	}
}

func (n *Node) serveTerm(conn net.Conn) {
	defer conn.Close()
	wait := n.lineWait
	if wait <= 0 {
		wait = termLineWait
	}
	_ = conn.SetDeadline(time.Now().Add(wait))
	line, err := readCRLF(conn, 80)
	if err != nil {
		return
	}
	// The dial can take much longer than the line that asked for it.
	_ = conn.SetDeadline(time.Time{})
	const prefix = "CONNECT "
	if !strings.HasPrefix(line, prefix) {
		fmt.Fprintf(conn, "ERR usage\n")
		return
	}
	call := strings.TrimSpace(line[len(prefix):])
	if call == "" || strings.ContainsAny(call, " \t") {
		fmt.Fprintf(conn, "ERR usage\n")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), n.dialFor)
	stream, err := n.Dial(ctx, call)
	cancel()
	_ = conn.SetDeadline(time.Now().Add(wait))
	if err != nil {
		fmt.Fprintf(conn, "ERR %s\n", termErr(err))
		return
	}
	defer stream.Close()
	if _, err := fmt.Fprintf(conn, "OK\n"); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stream, conn)
		_ = stream.Close()
		close(done)
	}()
	_, _ = io.Copy(conn, stream)
	_ = conn.Close()
	<-done
}

func termErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "connection timed out"
	}
	msg := err.Error()
	if msg == "" || len(msg) > 120 || strings.ContainsAny(msg, "\r\n") {
		return "The connection failed."
	}
	return msg
}

func readCRLF(r io.Reader, max int) (string, error) {
	var b []byte
	one := []byte{0}
	for {
		if _, err := r.Read(one); err != nil {
			return "", err
		}
		if one[0] == '\n' {
			return string(b), nil
		}
		if one[0] == '\r' {
			continue
		}
		if len(b) >= max {
			return "", errors.New("line is too long")
		}
		b = append(b, one[0])
	}
}

func servePrompt(local ax25.Address, st *ax25.Stream) {
	defer st.Close()
	name := local.String()
	if _, err := fmt.Fprintf(st, "Crossbeam %s\r%s>\r", name, name); err != nil {
		return
	}
	in := &crReader{r: st}
	for {
		line, err := in.read(236)
		if err != nil {
			if errors.Is(err, errLong) {
				fmt.Fprintf(st, "The line is too long.\r%s>\r", name)
				continue
			}
			return
		}
		switch strings.ToUpper(strings.TrimSpace(line)) {
		case "":
			fmt.Fprintf(st, "%s>\r", name)
		case "?", "HELP":
			fmt.Fprintf(st, "Commands:\r?        This list\rBYE      Disconnect\r%s>\r", name)
		case "BYE", "QUIT", "EXIT":
			fmt.Fprintf(st, "Bye\r")
			return
		default:
			fmt.Fprintf(st, "Unknown command. Send ? for help.\r%s>\r", name)
		}
	}
}

var errLong = errors.New("line is too long")

type crReader struct {
	r      io.Reader
	skipLF bool
}

func (c *crReader) read(max int) (string, error) {
	var b []byte
	one := []byte{0}
	for {
		if _, err := c.r.Read(one); err != nil {
			return "", err
		}
		ch := one[0]
		if c.skipLF {
			c.skipLF = false
			if ch == '\n' {
				continue
			}
		}
		if ch == '\r' || ch == '\n' {
			if ch == '\r' {
				c.skipLF = true
			}
			return string(b), nil
		}
		if len(b) == max {
			for {
				if _, err := c.r.Read(one); err != nil {
					return "", err
				}
				if one[0] == '\r' || one[0] == '\n' {
					if one[0] == '\r' {
						c.skipLF = true
					}
					return "", errLong
				}
			}
		}
		b = append(b, ch)
	}
}

func listenUnix(path string, mode os.FileMode) (net.Listener, error) {
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		conn, err := dialer.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil, errors.New("socket is already in use")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		ln.Close()
		_ = os.Remove(path)
		return nil, err
	}
	got, err := os.Stat(path)
	if err != nil {
		ln.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if got.Mode().Perm() != mode {
		ln.Close()
		_ = os.Remove(path)
		return nil, errors.New("socket mode was not applied")
	}
	return ln, nil
}
