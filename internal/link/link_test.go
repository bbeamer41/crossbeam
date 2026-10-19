// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

func TestPromptsBothWays(t *testing.T) {
	a := startNode(t, "K5LEB", true)
	b := startNode(t, "W1AW-1", true)
	mustPeers(t, a, b)
	mustPeers(t, b, a)

	fromA, err := sessionText(a, b.Local().String(), "?\rBYE\r")
	if err != nil || !strings.Contains(fromA, "Crossbeam W1AW-1\r") || !strings.Contains(fromA, "W1AW-1>\r") || !strings.Contains(fromA, "Commands:\r") || !strings.Contains(fromA, "Bye\r") {
		t.Fatalf("A saw %q %v", fromA, err)
	}
	fromB, err := sessionText(b, a.Local().String(), "?\rBYE\r")
	if err != nil || !strings.Contains(fromB, "Crossbeam K5LEB\r") || !strings.Contains(fromB, "K5LEB>\r") || !strings.Contains(fromB, "Bye\r") {
		t.Fatalf("B saw %q %v", fromB, err)
	}
}

func TestRetransmitAndEcho(t *testing.T) {
	a := startNode(t, "K5LEB", false)
	b := startNode(t, "W1AW-1", false)
	mustPeers(t, a, b)
	var once sync.Once
	b.mu.Lock()
	b.drop = func(frame ax25.Frame) bool {
		if !ax25.IsI(frame.Control) {
			return false
		}
		drop := false
		once.Do(func() { drop = true })
		return drop
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	stream, err := a.Dial(ctx, b.Local().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := readUntil(stream, "W1AW-1>\r", 3*time.Second)
	if err != nil || !strings.Contains(got, "W1AW-1>\r") {
		t.Fatalf("prompt %q %v", got, err)
	}

	echoA := startNode(t, "K5LEB-2", false)
	echoB := startEcho(t, "W1AW-2")
	mustPeers(t, echoA, echoB)
	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	stream, err = echoA.Dial(ctx, echoB.Local().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	payload := bytes.Repeat([]byte("The net meets Thursday.\r"), 40)
	if _, err := stream.Write(payload); err != nil {
		t.Fatal(err)
	}
	back, err := readN(stream, len(payload), 3*time.Second)
	if err != nil || !bytes.Equal(back, payload) {
		t.Fatalf("echo %d %v", len(back), err)
	}
}

func TestWirePeer(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := node.SetPeers("W1AW-1@" + peer.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- answerSABM(peer, node.Local(), mustCall(t, "W1AW-1"), []byte("W1AW-1>\r"))
	}()
	stream, err := node.Dial(ctx, "W1AW-1")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := readUntil(stream, "W1AW-1>\r", 2*time.Second)
	if err != nil || !strings.Contains(got, "W1AW-1>\r") {
		t.Fatalf("dial %q %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	other, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := other.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(other, 2, time.Second)
	if len(frames) < 2 || !ax25.IsUA(frames[0].Control) || frames[0].Command || !ax25.IsI(frames[1].Control) || !bytes.Contains(frames[1].Info, []byte("K5LEB>\r")) {
		t.Fatalf("answer %+v", frames)
	}
	rr := ax25.Frame{
		Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: false,
		Control: ax25.SControl(ax25.RR, ax25.NS(frames[1].Control)+1, false),
	}.Encode()
	if _, err := other.WriteToUDP(rr, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	sabme := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABME | 0x10}.Encode()
	if _, err := other.WriteToUDP(sabme, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames = readFrames(other, 1, time.Second)
	if len(frames) != 1 || !ax25.IsDM(frames[0].Control) {
		t.Fatalf("sabme %+v", frames)
	}

	bad := append([]byte(nil), sabm...)
	bad[len(bad)-1] ^= 0xFF
	if _, err := other.WriteToUDP(bad, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	other.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 2048)
	if _, _, err := other.ReadFromUDP(buf); err == nil {
		t.Fatal("bad frame got a reply")
	}
}

func TestPeerList(t *testing.T) {
	node := startNode(t, "K5LEB", true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := node.Dial(ctx, "W1AW-1"); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("dial %v", err)
	}
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT W1AW-1\n")
	conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "not listed") {
		t.Fatalf("terminal %q %v", line, err)
	}
}

func TestDisconnectWaitsForTheLastFrame(t *testing.T) {
	var mu sync.Mutex
	var got []byte
	done := make(chan struct{})
	b, err := Listen(Config{
		Bind:   "127.0.0.1:0",
		Local:  mustCall(t, "W1AW-1"),
		T1:     150 * time.Millisecond,
		N2:     6,
		Paclen: 128,
		Window: 4,
		Handler: func(_ ax25.Address, st *ax25.Stream) {
			defer close(done)
			buf := make([]byte, 64)
			var all []byte
			for {
				n, err := st.Read(buf)
				if n > 0 {
					all = append(all, buf[:n]...)
				}
				if err != nil {
					mu.Lock()
					got = append([]byte(nil), all...)
					mu.Unlock()
					return
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	a := startNode(t, "K5LEB", false)
	mustPeers(t, a, b)
	var once sync.Once
	a.mu.Lock()
	a.drop = func(frame ax25.Frame) bool {
		if !ax25.IsI(frame.Control) {
			return false
		}
		drop := false
		once.Do(func() { drop = true })
		return drop
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := a.Dial(ctx, b.Local().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write([]byte("Thursday\r")); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Contains(got, []byte("Thursday\r")) {
		t.Fatalf("lost last line %q", got)
	}
}

func TestTerminalWaitsOutTheHandshake(t *testing.T) {
	node := startNode(t, "K5LEB", true)
	node.lineWait = 200 * time.Millisecond
	peer := listenUDP(t, "127.0.0.1")
	if err := node.SetPeers("W1AW-1@" + peer.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		errc <- answerSABMAfter(peer, node.Local(), mustCall(t, "W1AW-1"), []byte("W1AW-1>\r"), 500*time.Millisecond)
	}()
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "OK\n" {
		t.Fatalf("handshake %q %v", line, err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestListedPeerRejectsAnotherAddress(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	real := listenUDP(t, "127.0.0.1")
	spoof := listenUDP(t, "127.0.0.2")
	if err := node.SetPeers("W1AW-1@127.0.0.1:9"); err != nil {
		t.Fatal(err)
	}
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "W1AW-1"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := spoof.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if frames := readFrames(spoof, 1, 200*time.Millisecond); len(frames) != 0 {
		t.Fatalf("spoof got %+v", frames)
	}
	if _, err := real.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(real, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("real peer %+v", frames)
	}
}

func TestUnlistedCallerStaysOnItsAddress(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	home := listenUDP(t, "127.0.0.1")
	other := listenUDP(t, "127.0.0.2")
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := home.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(home, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("home %+v", frames)
	}
	if _, err := other.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if frames := readFrames(other, 1, 200*time.Millisecond); len(frames) != 0 {
		t.Fatalf("retarget %+v", frames)
	}
}

func TestUnlistedCapPerAddress(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	home := listenUDP(t, "127.0.0.1")
	for i := 1; i <= maxGuestsPerIP; i++ {
		call := mustCall(t, fmt.Sprintf("N0C%d", i))
		sabm := ax25.Frame{Dest: node.Local(), Src: call, Command: true, Control: ax25.SABM | 0x10}.Encode()
		if _, err := home.WriteToUDP(sabm, node.LocalAddr()); err != nil {
			t.Fatal(err)
		}
		frames := readFrames(home, 2, time.Second)
		if !hasUA(frames) {
			t.Fatalf("guest %s %+v", call, frames)
		}
		ackInfo(t, home, node.LocalAddr(), frames)
	}
	extra := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CX"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := home.WriteToUDP(extra, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if frames := readFrames(home, 1, 200*time.Millisecond); len(frames) != 0 {
		t.Fatalf("over cap %+v", frames)
	}
	other := listenUDP(t, "127.0.0.2")
	if _, err := other.WriteToUDP(extra, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(other, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("other address %+v", frames)
	}
}

func TestUIDoesNotTakeASession(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	other := listenUDP(t, "127.0.0.1")
	for i := 0; i < ax25.MaxSessions+8; i++ {
		call := mustCall(t, fmt.Sprintf("N%05d", i))
		ui := ax25.Frame{Dest: node.Local(), Src: call, Command: true, Control: ax25.UI, PID: ax25.PIDText, Info: []byte("beacon")}.Encode()
		if _, err := other.WriteToUDP(ui, node.LocalAddr()); err != nil {
			t.Fatal(err)
		}
	}
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := other.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(other, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("after ui %+v", frames)
	}
}

func TestOversizedDatagramIsDropped(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	other := listenUDP(t, "127.0.0.1")
	big := bytes.Repeat([]byte{0x7e}, maxDatagram+50)
	if _, err := other.WriteToUDP(big, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := other.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(other, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("after oversized %+v", frames)
	}
}

func startNode(t *testing.T, call string, terminal bool) *Node {
	t.Helper()
	cfg := Config{
		Bind:   "127.0.0.1:0",
		Local:  mustCall(t, call),
		T1:     150 * time.Millisecond,
		N2:     6,
		Paclen: 128,
		Window: 4,
	}
	if terminal {
		cfg.TerminalPath = filepath.Join(t.TempDir(), "terminal.sock")
		cfg.TerminalMode = 0o600
	}
	node, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	if node.LocalAddr().IP.String() != "127.0.0.1" {
		t.Fatalf("bound %s", node.LocalAddr())
	}
	return node
}

func startEcho(t *testing.T, call string) *Node {
	t.Helper()
	node, err := Listen(Config{
		Bind:   "127.0.0.1:0",
		Local:  mustCall(t, call),
		T1:     150 * time.Millisecond,
		N2:     6,
		Paclen: 64,
		Window: 3,
		Handler: func(_ ax25.Address, st *ax25.Stream) {
			defer st.Close()
			buf := make([]byte, 512)
			for {
				n, err := st.Read(buf)
				if n > 0 {
					if _, werr := st.Write(buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}

func mustPeers(t *testing.T, from, to *Node) {
	t.Helper()
	if err := from.SetPeers(to.Local().String() + "@" + to.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
}

func mustCall(t *testing.T, call string) ax25.Address {
	t.Helper()
	addr, err := ax25.Parse(call)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func sessionText(node *Node, call, send string) (string, error) {
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s\n", call); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if line != "OK\n" {
		return line, fmt.Errorf("handshake %q", line)
	}
	if _, err := conn.Write([]byte(send)); err != nil {
		return "", err
	}
	text, err := readUntil(reader, "Bye\r", 3*time.Second)
	_, _ = io.Copy(io.Discard, reader)
	return text, err
}

func readUntil(r io.Reader, needle string, d time.Duration) (string, error) {
	got := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		tmp := make([]byte, 128)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				if strings.Contains(buf.String(), needle) {
					got <- buf.String()
					return
				}
			}
			if err != nil {
				fail <- fmt.Errorf("%w after %q", err, buf.String())
				return
			}
		}
	}()
	select {
	case text := <-got:
		return text, nil
	case err := <-fail:
		return "", err
	case <-time.After(d):
		return "", fmt.Errorf("timed out waiting for %q", needle)
	}
}

func readN(r io.Reader, n int, d time.Duration) ([]byte, error) {
	buf := make([]byte, n)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, buf)
		done <- err
	}()
	select {
	case err := <-done:
		return buf, err
	case <-time.After(d):
		return nil, fmt.Errorf("timed out")
	}
}

func listenUDP(t *testing.T, host string) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(host)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

func hasUA(frames []ax25.Frame) bool {
	for _, frame := range frames {
		if ax25.IsUA(frame.Control) {
			return true
		}
	}
	return false
}

func ackInfo(t *testing.T, pc *net.UDPConn, to *net.UDPAddr, frames []ax25.Frame) {
	t.Helper()
	for _, frame := range frames {
		if !ax25.IsI(frame.Control) {
			continue
		}
		rr := ax25.Frame{
			Dest: frame.Src, Src: frame.Dest, Command: false,
			Control: ax25.SControl(ax25.RR, (ax25.NS(frame.Control)+1)&7, false),
		}.Encode()
		if _, err := pc.WriteToUDP(rr, to); err != nil {
			t.Fatal(err)
		}
	}
}

func answerSABMAfter(pc *net.UDPConn, local, remote ax25.Address, info []byte, wait time.Duration) error {
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, from, err := pc.ReadFromUDP(buf)
	if err != nil {
		return err
	}
	frame, err := ax25.Decode(buf[:n])
	if err != nil {
		return err
	}
	if !frame.Dest.Equal(remote) || !frame.Src.Equal(local) || !ax25.IsSABM(frame.Control) || !frame.Command {
		return fmt.Errorf("sabm %+v", frame)
	}
	time.Sleep(wait)
	ua := ax25.Frame{Dest: local, Src: remote, Command: false, Control: ax25.UA | 0x10}.Encode()
	if _, err := pc.WriteToUDP(ua, from); err != nil {
		return err
	}
	data := ax25.Frame{
		Dest: local, Src: remote, Command: true,
		Control: ax25.IControl(0, 0, false), PID: ax25.PIDText, Info: info,
	}.Encode()
	_, err = pc.WriteToUDP(data, from)
	return err
}

func answerSABM(pc *net.UDPConn, local, remote ax25.Address, info []byte) error {
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, from, err := pc.ReadFromUDP(buf)
	if err != nil {
		return err
	}
	frame, err := ax25.Decode(buf[:n])
	if err != nil {
		return err
	}
	if !frame.Dest.Equal(remote) || !frame.Src.Equal(local) || !ax25.IsSABM(frame.Control) || !frame.Command {
		return fmt.Errorf("sabm %+v", frame)
	}
	ua := ax25.Frame{Dest: local, Src: remote, Command: false, Control: ax25.UA | 0x10}.Encode()
	if _, err := pc.WriteToUDP(ua, from); err != nil {
		return err
	}
	data := ax25.Frame{
		Dest: local, Src: remote, Command: true,
		Control: ax25.IControl(0, 0, false), PID: ax25.PIDText, Info: info,
	}.Encode()
	_, err = pc.WriteToUDP(data, from)
	return err
}

func TestTermErrHidesPaths(t *testing.T) {
	if termErr(errors.New("open /var/lib/crossbeam/crossbeam.db: sqlite busy")) != "The connection failed." {
		t.Fatal(termErr(errors.New("open /var/lib/crossbeam/crossbeam.db: sqlite busy")))
	}
	if termErr(errors.New("W1AW-1 is not listed")) != "W1AW-1 is not listed" {
		t.Fatal(termErr(errors.New("W1AW-1 is not listed")))
	}
}

func TestForwardUnlistedSkipsReady(t *testing.T) {
	ready := false
	node, err := Listen(Config{
		Bind: "127.0.0.1:0", Local: mustCall(t, "K5LEB"), T1: 150 * time.Millisecond, N2: 4,
		TerminalPath: filepath.Join(t.TempDir(), "terminal.sock"), TerminalMode: 0o600,
		Forward: func(context.Context, *ax25.Stream) (string, error) {
			return "sent 0 received 0", nil
		},
		ForwardReady: func(context.Context) error {
			ready = true
			return errors.New("article 1 title is 81 runes, above the bulletin limit of 80")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprintf(conn, "FORWARD W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "ERR W1AW-1 is not listed\n" || ready {
		t.Fatalf("reply %q ready %v err %v", reply, ready, err)
	}
}

func TestForwardClientCloseReleasesTheCall(t *testing.T) {
	peer, err := Listen(Config{
		Bind: "127.0.0.1:0", Local: mustCall(t, "W1AW-1"), T1: 150 * time.Millisecond, N2: 6, Paclen: 128,
		Handler: func(_ ax25.Address, st *ax25.Stream) {
			defer st.Close()
			buf := make([]byte, 1)
			_, _ = st.Read(buf)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	started := make(chan struct{})
	var once sync.Once
	master, err := Listen(Config{
		Bind: "127.0.0.1:0", Local: mustCall(t, "K5LEB"), T1: 150 * time.Millisecond, N2: 6, Paclen: 128,
		TerminalPath: filepath.Join(t.TempDir(), "terminal.sock"), TerminalMode: 0o600,
		Forward: func(_ context.Context, st *ax25.Stream) (string, error) {
			once.Do(func() { close(started) })
			buf := make([]byte, 1)
			_, err := st.Read(buf)
			return "", err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	mustPeers(t, master, peer)
	conn, err := net.Dial("unix", master.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(conn, "FORWARD %s\n", peer.Local()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("forward did not start")
	}
	conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var last error
	for {
		stream, err := master.Dial(ctx, peer.Local().String())
		if err == nil {
			stream.Close()
			return
		}
		last = err
		if ctx.Err() != nil {
			t.Fatal(last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestForwardWithoutExchange(t *testing.T) {
	node := startNode(t, "K5LEB", true)
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprintf(conn, "FORWARD W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "ERR forwarding is not available\n" {
		t.Fatalf("reply %q %v", reply, err)
	}
}

func readFrames(pc *net.UDPConn, want int, d time.Duration) []ax25.Frame {
	pc.SetReadDeadline(time.Now().Add(d))
	var out []ax25.Frame
	buf := make([]byte, 2048)
	for len(out) < want {
		n, _, err := pc.ReadFromUDP(buf)
		if err != nil {
			return out
		}
		frame, err := ax25.Decode(append([]byte(nil), buf[:n]...))
		if err != nil {
			continue
		}
		out = append(out, frame)
	}
	return out
}
