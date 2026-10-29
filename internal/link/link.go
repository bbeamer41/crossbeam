// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package link is the AX/UDP port, the optional KISS radio port, and the
// optional telnet and FBB TCP ports.
// Each UDP datagram is one AX.25 frame with its frame check sequence and
// without HDLC flags, the encapsulation in RFC 1226, carried on UDP.
// Port 10093 is the port other nodes expect. The bytes are readable text.
// The KISS port dials a TNC. That frame has no frame check sequence;
// the TNC adds it. The radio is 1200 baud.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/netrom"
	"github.com/bbeamer41/crossbeam/internal/settings"
)

const (
	maxDatagram    = 1024
	maxGuestsPerIP = 4
	guestHold      = 2 * time.Second
	termLineWait   = 5 * time.Second
	// setupHold keeps the return address of a SABM that the AX.25 engine
	// has not accepted yet. A slow KISS write can block that engine for
	// longer than guestHold.
	setupHold      = 10 * time.Minute
	peerReserveMax = 8
	// hopLinger is how long a relay crosslink stays up after the last
	// NET/ROM frame. A circuit this node opened keeps its crosslink until
	// the circuit closes.
	hopLinger      = 15 * time.Second
	maxNetromInbox = 64
	// dropLogGap is the quiet period before the same listed callsign
	// and address are logged again.
	dropLogGap = 30 * time.Second
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
	Handler      func(ax25.Address, io.ReadWriteCloser)
	// Forward runs one FBB exchange on a dialed session. The string is
	// the OK detail. Nil refuses the FORWARD terminal command.
	Forward func(context.Context, io.ReadWriteCloser) (string, error)
	// ForwardReady runs before a FORWARD dial. A non-nil error skips the dial.
	ForwardReady func(context.Context) error
	// Kiss is the TCP address of a KISS TNC. Empty keeps the radio off.
	Kiss string
	// KissRetry is the pause between TNC dials. Zero uses two seconds.
	KissRetry time.Duration
	// Telnet is the TCP address for telnet sessions. Empty keeps telnet off.
	Telnet string
	// TelnetPeers is the same text form as link.peers, for telnet calls.
	TelnetPeers string
	// FBB is the TCP address for raw FBB forwarding. Empty keeps it off.
	FBB string
	// FBBPeers is the same text form as link.peers, for FBB TCP calls.
	FBBPeers string
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

	mu    sync.Mutex
	peers map[string]*net.UDPAddr
	// peerPort is the UDP port used for an open session when the packet
	// arrived on a different port than link.peers. It is cleared when
	// the session ends, so the next outbound call uses the configured port.
	peerPort map[string]int
	// setupAddr is the source of a SABM waiting for the engine, or the
	// source that opened the current inbound session. Later UI frames
	// do not replace it. The guest sweeper does not own this map.
	setupAddr map[string]*net.UDPAddr
	setupAt   map[string]time.Time
	// live is a callsign whose AX.25 session has started and not ended.
	live     map[string]bool
	dropSeen map[string]time.Time
	guests   map[string]*guestRoute
	drop     func(ax25.Frame) bool
	terms    int
	forward  func(context.Context, io.ReadWriteCloser) (string, error)
	ready    func(context.Context) error

	routes *netrom.Router
	user   func(ax25.Address, io.ReadWriteCloser)
	nodes  ax25.Address
	hopMu  sync.Mutex
	hops   map[string]*hop

	netMu sync.Mutex
	inbox []netFrame
	wake  chan struct{}

	kissAddr  string
	kissWait  time.Duration
	kissMu    sync.Mutex
	kissWrite sync.Mutex
	kissConn  net.Conn

	telnetLn net.Listener
	fbbLn    net.Listener
	telnetTo map[string]string
	fbbTo    map[string]string
	tcpLive  map[net.Conn]struct{}
	telnetN  int
	fbbN     int
}

// netFrame is one PID CF information field waiting for the router.
// Frames stay in arrival order. The switch callback only enqueues them.
type netFrame struct {
	from ax25.Address
	info []byte
}

// hop is an AX.25 session that carries NET/ROM frames to one neighbor.
type hop struct {
	st      *ax25.Stream
	uses    int
	last    time.Time
	dialing bool
	done    chan struct{}
}

// Listen binds the UDP port and, when TerminalPath is set, the local
// terminal socket. Peers is the same text as the link.peers setting.
func Listen(cfg Config) (*Node, error) {
	if cfg.Local.Call == "" {
		return nil, errors.New("station callsign is empty")
	}
	bind := strings.TrimSpace(cfg.Bind)
	kissAddr := strings.TrimSpace(cfg.Kiss)
	telnetListen := strings.TrimSpace(cfg.Telnet)
	fbbListen := strings.TrimSpace(cfg.FBB)
	if bind == "" && kissAddr == "" && telnetListen == "" && fbbListen == "" &&
		strings.TrimSpace(cfg.TelnetPeers) == "" && strings.TrimSpace(cfg.FBBPeers) == "" {
		return nil, errors.New("link.listen is empty")
	}
	var pc *net.UDPConn
	if bind != "" {
		udpAddr, err := net.ResolveUDPAddr("udp", bind)
		if err != nil {
			return nil, err
		}
		pc, err = net.ListenUDP("udp", udpAddr)
		if err != nil {
			return nil, err
		}
	}
	if kissAddr != "" {
		resolved, err := settings.ResolveKiss(kissAddr, cfg.Lookup)
		if err != nil {
			if pc != nil {
				pc.Close()
			}
			return nil, err
		}
		kissAddr = resolved
	}
	mode := cfg.TerminalMode.Perm()
	if cfg.TerminalPath != "" && mode == 0 {
		mode = 0o600
	}
	if cfg.TerminalPath != "" && mode != 0o600 && mode != 0o660 {
		if pc != nil {
			pc.Close()
		}
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
		local:     cfg.Local,
		pc:        pc,
		lookup:    cfg.Lookup,
		dialFor:   t1 * time.Duration(n2+2),
		termPath:  cfg.TerminalPath,
		lineWait:  termLineWait,
		quit:      make(chan struct{}),
		peers:     map[string]*net.UDPAddr{},
		peerPort:  map[string]int{},
		setupAddr: map[string]*net.UDPAddr{},
		setupAt:   map[string]time.Time{},
		live:      map[string]bool{},
		dropSeen:  map[string]time.Time{},
		guests:    map[string]*guestRoute{},
		forward:   cfg.Forward,
		ready:     cfg.ForwardReady,
		nodes:     ax25.Address{Call: "NODES"},
		hops:      map[string]*hop{},
		wake:      make(chan struct{}, 1),
		kissAddr:  kissAddr,
		kissWait:  cfg.KissRetry,
		telnetTo:  map[string]string{},
		fbbTo:     map[string]string{},
		tcpLive:   map[net.Conn]struct{}{},
	}
	n.user = cfg.Handler
	if n.user == nil {
		local := cfg.Local
		n.user = func(_ ax25.Address, rw io.ReadWriteCloser) { servePrompt(local, rw) }
	}
	paclen := cfg.Paclen
	if paclen < 1 || paclen > ax25.DefaultPaclen {
		paclen = ax25.DefaultPaclen
	}
	n.routes = netrom.New(netrom.Config{
		Local:   cfg.Local,
		Payload: paclen - 20,
		Send:    n.sendNetrom,
		Accept:  n.acceptCircuit,
		Retain:  n.retainHop,
		Release: n.releaseHop,
	})
	if err := n.SetPeers(cfg.Peers); err != nil {
		n.routes.Close()
		if pc != nil {
			pc.Close()
		}
		return nil, err
	}
	n.sw = ax25.NewSwitch(ax25.Config{
		Local:  cfg.Local,
		Paclen: paclen,
		Window: cfg.Window,
		N2:     cfg.N2,
		T1:     cfg.T1,
		Send:   n.transmit,
		Accept: func(addr ax25.Address, st *ax25.Stream) { n.user(addr, n.gateStream(addr, st)) },
		Netrom: n.onNetrom,
		Begin:  n.beginSession,
		End:    n.endSession,
	})
	if pc != nil {
		go n.readLoop()
	}
	if kissAddr != "" {
		go n.kissLoop(kissAddr)
	}
	if err := n.rememberPeers(settings.KeyTelnetPeers, cfg.TelnetPeers, n.telnetTo); err != nil {
		n.Close()
		return nil, err
	}
	if err := n.rememberPeers(settings.KeyFBBPeers, cfg.FBBPeers, n.fbbTo); err != nil {
		n.Close()
		return nil, err
	}
	if err := n.bindTCP(telnetListen, fbbListen); err != nil {
		n.Close()
		return nil, err
	}
	go n.routeLoop()
	go n.maintain()
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
	if n.pc == nil {
		return nil
	}
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
	n.setupAddr = map[string]*net.UDPAddr{}
	n.setupAt = map[string]time.Time{}
	for call := range next {
		delete(n.guests, call)
	}
	n.mu.Unlock()
	n.syncNeighbors()
	n.broadcastRoutes()
	return nil
}

// Dial connects to a listed station and returns the byte stream.
func (n *Node) Dial(ctx context.Context, call string) (*ax25.Stream, error) {
	addr, err := n.listed(call)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sw.Dial(ctx, addr)
}

func (n *Node) listed(call string) (ax25.Address, error) {
	addr, err := ax25.Parse(call)
	if err != nil {
		return ax25.Address{}, err
	}
	n.mu.Lock()
	_, ok := n.peers[addr.String()]
	n.mu.Unlock()
	if !ok {
		return ax25.Address{}, fmt.Errorf("%s is not listed", addr)
	}
	return addr, nil
}

// Close stops the UDP port, the terminal socket, and open sessions.
func (n *Node) Close() error {
	n.once.Do(func() {
		close(n.quit)
		n.kissMu.Lock()
		kconn := n.kissConn
		n.kissConn = nil
		n.kissMu.Unlock()
		if kconn != nil {
			_ = kconn.Close()
		}
		if n.telnetLn != nil {
			_ = n.telnetLn.Close()
		}
		if n.fbbLn != nil {
			_ = n.fbbLn.Close()
		}
		n.mu.Lock()
		live := n.tcpLive
		n.tcpLive = map[net.Conn]struct{}{}
		n.mu.Unlock()
		for conn := range live {
			_ = conn.Close()
		}
		if n.routes != nil {
			n.routes.Close()
		}
		n.hopMu.Lock()
		hops := n.hops
		n.hops = map[string]*hop{}
		n.hopMu.Unlock()
		for _, hop := range hops {
			if hop.st != nil {
				_ = hop.st.Close()
			}
		}
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
		if err != nil || len(frame.Digis) > 0 {
			continue
		}
		if ax25.IsUI(frame.Control) {
			n.hearBroadcast(frame, from)
			continue
		}
		if !frame.Dest.Equal(n.local) {
			continue
		}
		if !n.allow(frame.Src, from) {
			continue
		}
		if ax25.IsSABM(frame.Control) {
			n.rememberSetup(frame.Src, from)
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
	// A station that has a UDP route stays on UDP. A closed socket does
	// not move that station onto the radio.
	if addr != nil {
		if n.pc != nil {
			_, _ = n.pc.WriteToUDP(frame.Encode(), addr)
		}
		return
	}
	if n.kissAddr != "" {
		n.writeKiss(frame)
	}
}

func (n *Node) onNetrom(from ax25.Address, info []byte) {
	n.netMu.Lock()
	// The AX.25 engine has already accepted the frame. Past this cap the
	// frame is dropped so a fast neighbor cannot grow memory without bound.
	if len(n.inbox) < maxNetromInbox {
		n.inbox = append(n.inbox, netFrame{from: from, info: append([]byte(nil), info...)})
	}
	n.netMu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// routeLoop delivers PID CF frames in the order the switch accepted them.
// One worker keeps a disconnect from passing the information ahead of it.
func (n *Node) routeLoop() {
	for {
		select {
		case <-n.quit:
			return
		case <-n.wake:
			for {
				from, info, ok := n.takeNetrom()
				if !ok {
					break
				}
				if n.routes != nil {
					n.routes.Deliver(from, info)
				}
			}
		}
	}
}

func (n *Node) takeNetrom() (ax25.Address, []byte, bool) {
	n.netMu.Lock()
	defer n.netMu.Unlock()
	if len(n.inbox) == 0 {
		return ax25.Address{}, nil, false
	}
	item := n.inbox[0]
	n.inbox[0] = netFrame{}
	n.inbox = n.inbox[1:]
	if len(n.inbox) == 0 {
		n.inbox = nil
	}
	return item.from, item.info, true
}

func (n *Node) acceptCircuit(user ax25.Address, rw io.ReadWriteCloser) {
	if n.user == nil {
		_ = rw.Close()
		return
	}
	// A NET/ROM circuit is not a bulletin forward. The prompt still runs.
	call := user.String()
	if call == "" {
		call = "unknown"
	}
	n.user(user, &sessionGate{ReadWriteCloser: rw, allow: false, label: call + " netrom"})
}

// sessionGate is the optional caller mark read by bulletin.Serve.
// ForwardAllowed is false for an unlisted AX/UDP guest, an unlisted TCP
// address, and a NET/ROM circuit. A stream without these methods is left
// alone so a raw pipe in a test can still exchange bulletins.
type sessionGate struct {
	io.ReadWriteCloser
	allow bool
	label string
}

func (g *sessionGate) ForwardAllowed() bool { return g.allow }

func (g *sessionGate) PeerLabel() string { return g.label }

func (n *Node) gateStream(addr ax25.Address, st io.ReadWriteCloser) io.ReadWriteCloser {
	allow, label := n.forwardPolicy(addr)
	return &sessionGate{ReadWriteCloser: st, allow: allow, label: label}
}

// forwardPolicy accepts an AX/UDP forward from a listed station. A guest
// route is refused. A station with no UDP route is on the radio when the
// TNC is configured, and that session may still forward.
func (n *Node) forwardPolicy(addr ax25.Address) (bool, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	call := addr.String()
	if call == "" {
		call = "unknown"
	}
	udp := n.lookupAddr(addr)
	where := ""
	if udp != nil {
		where = " " + udp.String()
	}
	if _, ok := n.peers[addr.String()]; ok {
		return true, call + where
	}
	if _, ok := n.guests[addr.String()]; ok {
		return false, call + where
	}
	if n.kissAddr != "" && udp == nil {
		return true, call + " radio"
	}
	return false, call + where
}

func (n *Node) syncNeighbors() {
	if n.routes == nil {
		return
	}
	n.mu.Lock()
	list := make([]ax25.Address, 0, len(n.peers))
	for call := range n.peers {
		addr, err := ax25.Parse(call)
		if err == nil {
			list = append(list, addr)
		}
	}
	n.mu.Unlock()
	n.routes.SetNeighbors(list)
}

func (n *Node) broadcastRoutes() {
	if n.routes == nil || n.pc == nil {
		return
	}
	payloads := n.routes.Payloads()
	n.mu.Lock()
	peers := make([]*net.UDPAddr, 0, len(n.peers))
	for _, addr := range n.peers {
		peers = append(peers, &net.UDPAddr{IP: append(net.IP(nil), addr.IP...), Port: addr.Port})
	}
	n.mu.Unlock()
	if len(peers) == 0 {
		return
	}
	for _, info := range payloads {
		frame := ax25.Frame{
			Dest:    n.nodes,
			Src:     n.local,
			Command: true,
			Control: ax25.UI,
			PID:     ax25.PIDNetrom,
			Info:    info,
		}.Encode()
		for _, addr := range peers {
			_, _ = n.pc.WriteToUDP(frame, addr)
		}
	}
}

func (n *Node) hearBroadcast(frame ax25.Frame, from *net.UDPAddr) {
	if n.routes == nil || frame.PID != ax25.PIDNetrom || !frame.Dest.Equal(n.nodes) {
		return
	}
	if frame.Src.Equal(n.local) || !n.isPeer(frame.Src) {
		return
	}
	if !n.allow(frame.Src, from) {
		return
	}
	n.routes.Observe(frame.Src, frame.Info)
}

func (n *Node) isPeer(src ax25.Address) bool {
	n.mu.Lock()
	_, ok := n.peers[src.String()]
	n.mu.Unlock()
	return ok
}

func (n *Node) maintain() {
	sweep := time.NewTicker(500 * time.Millisecond)
	hour := time.NewTicker(time.Hour)
	defer sweep.Stop()
	defer hour.Stop()
	for {
		select {
		case <-n.quit:
			return
		case <-sweep.C:
			n.mu.Lock()
			n.sweepGuestsLocked(time.Now())
			n.mu.Unlock()
			n.sweepHops()
		case <-hour.C:
			if n.routes != nil {
				n.routes.Age()
			}
			n.broadcastRoutes()
		}
	}
}

func (n *Node) sendNetrom(via ax25.Address, info []byte) error {
	if n.sw == nil {
		return errors.New("link is closed")
	}
	err := n.sw.SendNetrom(via, info)
	if err == nil {
		n.touchHop(via)
		return nil
	}
	if !errors.Is(err, ax25.ErrNotConnected) && !errors.Is(err, ax25.ErrClosed) {
		return err
	}
	n.hopMu.Lock()
	if h := n.hops[via.String()]; h != nil {
		h.st = nil
	}
	n.hopMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), n.dialFor)
	defer cancel()
	if err := n.ensureHop(ctx, via); err != nil {
		return err
	}
	if err := n.sw.SendNetrom(via, info); err != nil {
		return err
	}
	n.touchHop(via)
	return nil
}

func (n *Node) retainHop(ctx context.Context, via ax25.Address) error {
	if err := n.ensureHop(ctx, via); err != nil {
		return err
	}
	n.hopMu.Lock()
	if h := n.hops[via.String()]; h != nil {
		h.uses++
		h.last = time.Now()
	}
	n.hopMu.Unlock()
	return nil
}

func (n *Node) releaseHop(via ax25.Address) {
	n.hopMu.Lock()
	h := n.hops[via.String()]
	if h == nil {
		n.hopMu.Unlock()
		return
	}
	if h.uses > 0 {
		h.uses--
	}
	var st *ax25.Stream
	if h.uses == 0 && !h.dialing {
		st = h.st
		delete(n.hops, via.String())
	}
	n.hopMu.Unlock()
	if st != nil {
		_ = st.Close()
	}
}

func (n *Node) ensureHop(ctx context.Context, via ax25.Address) error {
	key := via.String()
	for {
		n.hopMu.Lock()
		h := n.hops[key]
		if h != nil && h.st != nil {
			h.last = time.Now()
			n.hopMu.Unlock()
			return nil
		}
		if h != nil && h.dialing {
			done := h.done
			n.hopMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if h == nil {
			h = &hop{}
			n.hops[key] = h
		}
		h.dialing = true
		h.done = make(chan struct{})
		n.hopMu.Unlock()

		st, err := n.sw.Dial(ctx, via)
		n.hopMu.Lock()
		h.dialing = false
		close(h.done)
		if err != nil {
			if strings.Contains(err.Error(), "already connected") {
				h.last = time.Now()
				n.hopMu.Unlock()
				return nil
			}
			if h.st == nil && h.uses == 0 {
				delete(n.hops, key)
			}
			n.hopMu.Unlock()
			return err
		}
		h.st = st
		h.last = time.Now()
		n.hopMu.Unlock()
		go n.drainHop(st)
		return nil
	}
}

func (n *Node) drainHop(st *ax25.Stream) {
	buf := make([]byte, 512)
	for {
		if _, err := st.Read(buf); err != nil {
			return
		}
	}
}

func (n *Node) touchHop(via ax25.Address) {
	n.hopMu.Lock()
	if h := n.hops[via.String()]; h != nil {
		h.last = time.Now()
	}
	n.hopMu.Unlock()
}

func (n *Node) sweepHops() {
	now := time.Now()
	n.hopMu.Lock()
	var drop []*ax25.Stream
	for key, h := range n.hops {
		if h.dialing || h.uses > 0 || h.st == nil || h.last.IsZero() || now.Sub(h.last) < hopLinger {
			continue
		}
		drop = append(drop, h.st)
		delete(n.hops, key)
	}
	n.hopMu.Unlock()
	for _, st := range drop {
		_ = st.Close()
	}
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
			n.noteWrongIPLocked(key, from, now)
			return false
		}
		if n.live[key] {
			n.setSessionPortLocked(key, peer.Port, from.Port)
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
	for key, at := range n.setupAt {
		if n.live[key] || now.Sub(at) < setupHold {
			continue
		}
		delete(n.setupAddr, key)
		delete(n.setupAt, key)
	}
	for key, guest := range n.guests {
		if guest.active || now.Sub(guest.seen) < guestHold {
			continue
		}
		if at, ok := n.setupAt[key]; ok && now.Sub(at) < setupHold {
			continue
		}
		delete(n.guests, key)
	}
}

// rememberSetup stores the source of this SABM. The first one waits
// until the engine accepts it. A NODES broadcast cannot move it.
func (n *Node) rememberSetup(src ax25.Address, from *net.UDPAddr) {
	ip := canonicalIP(from.IP)
	if ip == nil || from.Port < 1 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.setupAddr == nil {
		n.setupAddr = map[string]*net.UDPAddr{}
		n.setupAt = map[string]time.Time{}
	}
	key := src.String()
	if _, waiting := n.setupAddr[key]; waiting && !n.live[key] {
		n.setupAt[key] = time.Now()
		return
	}
	n.setupAddr[key] = &net.UDPAddr{IP: append(net.IP(nil), ip...), Port: from.Port}
	n.setupAt[key] = time.Now()
}

func (n *Node) beginSession(remote ax25.Address, inbound bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := remote.String()
	n.live[key] = true
	// An outbound call keeps the configured port for its SABM. The peer's
	// reply, handled in allow, learns a different source port for this
	// session only. An inbound SABM uses the port stored for that SABM.
	if !inbound {
		delete(n.peerPort, key)
		delete(n.setupAddr, key)
		delete(n.setupAt, key)
		return
	}
	from := n.setupAddr[key]
	if from != nil {
		if peer, listed := n.peers[key]; listed {
			n.setSessionPortLocked(key, peer.Port, from.Port)
		} else if guest, ok := n.guests[key]; ok {
			guest.addr = cloneUDP(from)
			guest.seen = time.Now()
		} else {
			n.guests[key] = &guestRoute{addr: cloneUDP(from), seen: time.Now()}
		}
	}
	if guest, ok := n.guests[key]; ok {
		guest.active = true
	}
}

func cloneUDP(from *net.UDPAddr) *net.UDPAddr {
	return &net.UDPAddr{IP: append(net.IP(nil), from.IP...), Port: from.Port}
}

func (n *Node) endSession(remote ax25.Address) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := remote.String()
	delete(n.live, key)
	delete(n.peerPort, key)
	delete(n.setupAddr, key)
	delete(n.setupAt, key)
	if _, ok := n.guests[key]; !ok {
		return
	}
	delete(n.guests, key)
}

func (n *Node) setSessionPortLocked(key string, configured, port int) {
	if port != configured {
		n.peerPort[key] = port
		return
	}
	delete(n.peerPort, key)
}

func (n *Node) noteWrongIPLocked(call string, from *net.UDPAddr, now time.Time) {
	ip := canonicalIP(from.IP)
	if ip == nil {
		return
	}
	mark := call + " " + ip.String()
	if prev, ok := n.dropSeen[mark]; ok && now.Sub(prev) < dropLogGap {
		return
	}
	if len(n.dropSeen) >= 32 {
		n.dropSeen = map[string]time.Time{}
	}
	n.dropSeen[mark] = now
	log.Printf("crossbeam: dropped %s from %s", call, from.String())
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
	kind, call := terminalCommand(line)
	if kind == "" || call == "" || strings.ContainsAny(call, " \t") {
		fmt.Fprintf(conn, "ERR usage\n")
		return
	}
	if kind == "forward" && n.forward == nil {
		_ = conn.SetDeadline(time.Now().Add(wait))
		fmt.Fprintf(conn, "ERR forwarding is not available\n")
		return
	}
	if kind == "forward" {
		n.serveForward(conn, call, wait)
		return
	}
	addr, err := ax25.Parse(call)
	if err != nil {
		_ = conn.SetDeadline(time.Now().Add(wait))
		fmt.Fprintf(conn, "ERR %s\n", termErr(err))
		return
	}
	var rw io.ReadWriteCloser
	_, listedErr := n.listed(call)
	telnetTarget, telnetOK := n.tcpTarget(n.telnetTo, call)
	routed := false
	if n.routes != nil {
		if _, quality, ok := n.routes.Lookup(addr); ok && quality > 0 {
			routed = true
		}
	}
	switch {
	case listedErr == nil:
		ctx, cancel := context.WithTimeout(context.Background(), n.dialFor)
		rw, err = n.Dial(ctx, call)
		cancel()
	case telnetOK:
		ctx, cancel := context.WithTimeout(context.Background(), n.dialFor)
		rw, err = n.dialTelnet(ctx, telnetTarget)
		cancel()
	case routed:
		ctx, cancel := context.WithTimeout(context.Background(), n.dialFor*3)
		rw, err = n.routes.Connect(ctx, addr)
		cancel()
	case n.kissAddr != "":
		ctx, cancel := context.WithTimeout(context.Background(), n.dialFor)
		rw, err = n.dialRadio(ctx, addr)
		cancel()
	default:
		err = fmt.Errorf("%s is not listed", addr)
	}
	_ = conn.SetDeadline(time.Now().Add(wait))
	if err != nil {
		fmt.Fprintf(conn, "ERR %s\n", termErr(err))
		return
	}
	defer rw.Close()
	if _, err := fmt.Fprintf(conn, "OK\n"); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() {
		// A half-close means the user has finished typing. The station
		// at the far end still has to answer, so the AX.25 session stays
		// up. Closing the terminal socket entirely ends the session.
		_, err := io.Copy(rw, conn)
		if err != nil || clientLeft(conn) {
			_ = rw.Close()
		}
		close(done)
	}()
	_, _ = io.Copy(conn, rw)
	_ = conn.Close()
	<-done
}

// serveForward dials one listed station and runs the FBB exchange.
// If the terminal client goes away, the AX.25 session is closed.
// Cancelling the context does not unblock a stream read, so the
// session itself is closed.
func (n *Node) serveForward(conn net.Conn, call string, wait time.Duration) {
	_, listedErr := n.listed(call)
	fbbTarget, fbbOK := n.tcpTarget(n.fbbTo, call)
	if listedErr != nil && !fbbOK {
		_ = conn.SetDeadline(time.Now().Add(wait))
		fmt.Fprintf(conn, "ERR %s\n", termErr(listedErr))
		return
	}
	fctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		cancel()
		close(clientDone)
	}()
	if n.ready != nil {
		if err := n.ready(fctx); err != nil {
			_ = conn.SetDeadline(time.Now().Add(wait))
			fmt.Fprintf(conn, "ERR %s\n", termErr(err))
			return
		}
	}
	select {
	case <-clientDone:
		return
	default:
	}
	// fctx ends when the terminal client goes away, so a dial still in
	// setup releases the callsign instead of running out its timer.
	dctx, dcancel := context.WithTimeout(fctx, n.dialFor)
	var stream io.ReadWriteCloser
	var err error
	if listedErr == nil {
		stream, err = n.Dial(dctx, call)
	} else {
		stream, err = n.dialFBB(dctx, fbbTarget)
	}
	dcancel()
	_ = conn.SetDeadline(time.Now().Add(wait))
	if err != nil {
		fmt.Fprintf(conn, "ERR %s\n", termErr(err))
		return
	}
	defer stream.Close()
	select {
	case <-clientDone:
		return
	default:
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-clientDone:
			_ = stream.Close()
		case <-stop:
		}
	}()
	_ = conn.SetDeadline(time.Time{})
	detail, err := n.forward(fctx, stream)
	_ = conn.SetDeadline(time.Now().Add(wait))
	if err != nil {
		fmt.Fprintf(conn, "ERR %s\n", termErr(err))
		return
	}
	if !showTerminal(detail, 80) {
		detail = "forward complete"
	}
	fmt.Fprintf(conn, "OK %s\n", detail)
}

func terminalCommand(line string) (string, string) {
	switch {
	case strings.HasPrefix(line, "CONNECT "):
		return "connect", strings.TrimSpace(line[len("CONNECT "):])
	case strings.HasPrefix(line, "FORWARD "):
		return "forward", strings.TrimSpace(line[len("FORWARD "):])
	default:
		return "", ""
	}
}

func termErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "connection timed out"
	}
	if err == nil || !showTerminal(err.Error(), 120) {
		return "The connection failed."
	}
	return err.Error()
}

// showTerminal reports whether text can be written to the terminal.
// Paths, database details, and control bytes stay off that line.
func showTerminal(msg string, max int) bool {
	if msg == "" || len(msg) > max || strings.ContainsAny(msg, "\r\n") {
		return false
	}
	for i := 0; i < len(msg); i++ {
		if msg[i] < 0x20 || msg[i] > 0x7e {
			return false
		}
	}
	lower := strings.ToLower(msg)
	for _, bad := range []string{"sqlite", ".db", "/home/", "/var/", "password", "token"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	return true
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

func servePrompt(local ax25.Address, st io.ReadWriteCloser) {
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
