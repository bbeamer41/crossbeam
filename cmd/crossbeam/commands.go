// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/admin"
	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/bulletin"
	"github.com/bbeamer41/crossbeam/internal/link"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"golang.org/x/term"
)

const defaultState = "crossbeam-state"

func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	listen := fs.String("listen", "", "address for this start only; a public address still requires admin.public")
	linkFlag := fs.String("link", "", "AX/UDP address for this start only; does not change the saved setting")
	peersFlag := fs.String("peers", "", "peer list for this start only; does not change the saved setting")
	socket := fs.String("socket", "", "unix socket path")
	modeText := fs.String("socket-mode", "0600", "0600 or 0660")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: serve does not take arguments")
		return 2
	}
	mode, err := admin.ParseSocketMode(*modeText)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 2
	}
	st, err := store.Open(*state)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	defer st.Close()
	has, err := st.HasPassword(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	if !has {
		fmt.Fprintf(stderr, "crossbeam: no sysop password is set. Run: crossbeam sysop password --state %s\n", *state)
		return 78
	}
	running, err := admin.Start(ctx, st, admin.Config{
		Listen:     *listen,
		SocketPath: *socket,
		SocketMode: mode,
	})
	if errors.Is(err, store.ErrNoPassword) {
		fmt.Fprintf(stderr, "crossbeam: no sysop password is set. Run: crossbeam sysop password --state %s\n", *state)
		return 78
	}
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	node, err := startLink(ctx, st, running.SocketPath, mode, fs, *linkFlag, *peersFlag)
	if err != nil {
		running.Close()
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	if node != nil {
		defer node.Close()
	}
	fmt.Fprintf(stdout, "crossbeam %s\n%s\nunix socket %s\ncertificate sha256 %s\n", version, running.URL, running.SocketPath, running.CertSHA256)
	if node != nil {
		fmt.Fprintf(stdout, "ax/udp %s\nterminal socket %s\n", node.LocalAddr(), node.Terminal())
	}
	err = waitServe(ctx, running)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	return 0
}

func startLink(ctx context.Context, st *store.Store, socketPath string, mode os.FileMode, fs *flag.FlagSet, linkFlag, peersFlag string) (*link.Node, error) {
	current, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	linkText := current[settings.KeyLinkListen]
	peersText := current[settings.KeyLinkPeers]
	allowZero := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "link":
			linkText = linkFlag
			allowZero = true
		case "peers":
			peersText = peersFlag
		}
	})
	if strings.TrimSpace(linkText) == "" {
		return nil, nil
	}
	bind, err := settings.ResolveLink(linkText, nil, allowZero)
	if err != nil {
		return nil, err
	}
	call, err := ax25.Parse(current[settings.KeyCallsign])
	if err != nil {
		return nil, errors.New("station.callsign must be an AX.25 callsign when the link is on")
	}
	return link.Listen(link.Config{
		Bind:         bind,
		Local:        call,
		Peers:        peersText,
		TerminalPath: filepath.Join(filepath.Dir(socketPath), "terminal.sock"),
		TerminalMode: mode,
		Handler:      bulletin.Answer(st, call),
		ForwardReady: func(ctx context.Context) error {
			return bulletin.Check(ctx, st, call)
		},
		Forward: func(ctx context.Context, stream *ax25.Stream) (string, error) {
			res, err := bulletin.Forward(ctx, st, call, stream)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("sent %d received %d", res.Sent, res.Received), nil
		},
	})
}

func cmdConnect(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "terminal socket path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam connect CALL [--state DIR | --socket PATH]")
		return 2
	}
	if _, err := ax25.Parse(fs.Arg(0)); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 2
	}
	path := *socket
	if path == "" {
		path = filepath.Join(*state, "terminal.sock")
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AX/UDP link is not running")
		return 1
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	if _, err := fmt.Fprintf(conn, "CONNECT %s\n", fs.Arg(0)); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	_ = conn.SetDeadline(time.Now().Add(70 * time.Second))
	reply, err := readConnLine(conn)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AX/UDP link is not running")
		return 1
	}
	_ = conn.SetDeadline(time.Time{})
	if strings.HasPrefix(reply, "ERR ") {
		fmt.Fprintf(stderr, "crossbeam: %s\n", reply[4:])
		return 1
	}
	if reply != "OK" {
		fmt.Fprintln(stderr, "crossbeam: the link did not accept the connection")
		return 1
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return connectTerminal(file, conn, stdout, stderr)
	}
	go func() { _, _ = io.Copy(conn, stdin) }()
	if _, err := io.Copy(stdout, conn); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	return 0
}

func cmdBulletin(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "send" {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam bulletin send CALL [--state DIR | --socket PATH]")
		if len(args) == 1 && isHelp(args[0]) {
			return 0
		}
		return 2
	}
	fs := flag.NewFlagSet("bulletin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "terminal socket path")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam bulletin send CALL [--state DIR | --socket PATH]")
		return 2
	}
	if _, err := ax25.Parse(fs.Arg(0)); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 2
	}
	path := *socket
	if path == "" {
		path = filepath.Join(*state, "terminal.sock")
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AX/UDP link is not running")
		return 1
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	if _, err := fmt.Fprintf(conn, "FORWARD %s\n", fs.Arg(0)); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	_ = conn.SetDeadline(time.Now().Add(120 * time.Second))
	reply, err := readConnLine(conn)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AX/UDP link is not running")
		return 1
	}
	if strings.HasPrefix(reply, "ERR ") {
		fmt.Fprintf(stderr, "crossbeam: %s\n", reply[4:])
		return 1
	}
	if !strings.HasPrefix(reply, "OK ") {
		fmt.Fprintln(stderr, "crossbeam: the link did not accept the forward")
		return 1
	}
	fmt.Fprintln(stdout, reply[3:])
	return 0
}

func connectTerminal(in *os.File, conn net.Conn, stdout, stderr io.Writer) int {
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	defer term.Restore(int(in.Fd()), old)
	done := make(chan struct{})
	go func() {
		one := []byte{0}
		var last byte
		for {
			if _, err := conn.Read(one); err != nil {
				close(done)
				return
			}
			switch one[0] {
			case '\r':
				_, _ = stdout.Write([]byte("\r\n"))
			case '\n':
				if last != '\r' {
					_, _ = stdout.Write([]byte("\r\n"))
				}
			default:
				_, _ = stdout.Write(one)
			}
			last = one[0]
		}
	}()
	one := []byte{0}
	var line []byte
	for {
		if _, err := in.Read(one); err != nil {
			break
		}
		switch one[0] {
		case 3, 4:
			return 0
		case '\r', '\n':
			_, _ = stdout.Write([]byte("\r\n"))
			line = append(line, '\r')
			if _, err := conn.Write(line); err != nil {
				return 1
			}
			line = line[:0]
		case 8, 127:
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = stdout.Write([]byte("\b \b"))
			}
		default:
			if one[0] >= 32 && one[0] < 127 && len(line) < 236 {
				line = append(line, one[0])
				_, _ = stdout.Write(one)
			}
		}
	}
	<-done
	return 0
}

func readConnLine(conn net.Conn) (string, error) {
	var b []byte
	one := []byte{0}
	for {
		if _, err := conn.Read(one); err != nil {
			return "", err
		}
		if one[0] == '\n' {
			return string(b), nil
		}
		if one[0] == '\r' {
			continue
		}
		if len(b) >= 200 {
			return "", errors.New("reply is too long")
		}
		b = append(b, one[0])
	}
}

func waitServe(ctx context.Context, running *admin.Running) error {
	done := make(chan error, 1)
	go func() { done <- running.Wait() }()
	select {
	case <-ctx.Done():
		running.Close()
		return <-done
	case err := <-done:
		running.Close()
		return err
	}
}

func cmdSysop(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "password" || (len(args) > 1 && isHelp(args[1])) {
		if len(args) == 1 && isHelp(args[0]) {
			fmt.Fprintln(stderr, "crossbeam: usage: crossbeam sysop password [--state DIR]")
			return 0
		}
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam sysop password [--state DIR]")
		return 2
	}
	fs := flag.NewFlagSet("sysop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam sysop password [--state DIR]")
		return 2
	}
	password, err := readPassword(stdin, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	st, err := store.Open(*state)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	defer st.Close()
	if err := st.SetPassword(ctx, password); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Sysop password saved.")
	return 0
}

func readPassword(stdin io.Reader, stderr io.Writer) (string, error) {
	fmt.Fprint(stderr, "Sysop password: ")
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		password, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		if len(password) > 256 {
			return "", errors.New("password is too long")
		}
		return trimCRLF(string(password)), nil
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 257))
	if err != nil {
		return "", err
	}
	if len(raw) > 256 {
		return "", errors.New("password is too long")
	}
	return trimCRLF(string(raw)), nil
}

func trimCRLF(value string) string {
	for len(value) > 0 {
		last := value[len(value)-1]
		if last != '\n' && last != '\r' {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}

func cmdSettings(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sub, rest := splitSub(args)
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	back, code := openBackend(fs, *state, *socket, stderr)
	if code != 0 {
		return code
	}
	defer back.close()
	switch sub {
	case "":
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "crossbeam: usage: crossbeam settings [--state DIR | --socket PATH]")
			return 2
		}
		current, err := back.settings(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		for _, field := range settings.Schema {
			fmt.Fprintf(stdout, "%s=%s\n", field.Key, current[field.Key])
		}
		return 0
	case "get":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "crossbeam: usage: crossbeam settings get KEY")
			return 2
		}
		key := fs.Arg(0)
		if _, ok := settings.ByKey(key); !ok {
			fmt.Fprintf(stderr, "crossbeam: unknown setting %q\n", key)
			return 1
		}
		current, err := back.settings(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		fmt.Fprintln(stdout, current[key])
		return 0
	case "set":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, "crossbeam: usage: crossbeam settings set KEY VALUE")
			return 2
		}
		if _, err := back.apply(ctx, map[string]string{fs.Arg(0): fs.Arg(1)}); err != nil {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam settings [get KEY | set KEY VALUE]")
		return 2
	}
}

func cmdStatus(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: status does not take arguments")
		return 2
	}
	back, code := openBackend(fs, *state, *socket, stderr)
	if code != 0 {
		return code
	}
	defer back.close()
	if back.client != nil {
		res, err := back.do(ctx, http.MethodGet, "/api/status", nil)
		if err != nil {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			fmt.Fprintf(stderr, "crossbeam: %s\n", httpErr(res))
			return 1
		}
		var body struct {
			Version string `json:"version"`
			Listen  string `json:"listen"`
			Public  bool   `json:"public"`
			URL     string `json:"url"`
			Cert    string `json:"cert_sha256"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "crossbeam %s\nlisten: %s\npublic: %t\nurl: %s\ncertificate sha256: %s\n", body.Version, body.Listen, body.Public, body.URL, body.Cert)
		return 0
	}
	current, err := back.st.Settings(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	listen := current[settings.KeyListen]
	if listen == "" {
		listen = "(chosen at start)"
	}
	sha := "(none)"
	if fp, err := admin.Fingerprint(back.state); err == nil {
		sha = fp
	}
	fmt.Fprintf(stdout, "crossbeam %s\nlisten: %s\npublic: %s\nurl: (running process)\ncertificate sha256: %s\n", version, listen, current[settings.KeyPublic], sha)
	return 0
}

func cmdArticle(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam article preview|save|list|delete")
		return 2
	}
	switch args[0] {
	case "preview":
		return cmdPreview(args[1:], stdout, stderr)
	case "save":
		return cmdSave(ctx, args[1:], stdout, stderr)
	case "list":
		return cmdList(ctx, args[1:], stdout, stderr)
	case "delete":
		return cmdDelete(ctx, args[1:], stdout, stderr)
	default:
		return unknown(append([]string{"article"}, args...), stderr)
	}
}

func cmdDelete(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam article delete ROW [--state DIR]")
		return 2
	}
	row, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil || row <= 0 {
		fmt.Fprintln(stderr, "crossbeam: article row must be a positive integer")
		return 2
	}
	st, err := store.Open(*state)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the article could not be deleted")
		return 1
	}
	defer st.Close()
	if err := st.DeleteArticle(ctx, row); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintln(stderr, "crossbeam: article not found")
			return 1
		}
		fmt.Fprintln(stderr, "crossbeam: the article could not be deleted")
		return 1
	}
	fmt.Fprintf(stdout, "deleted %d\n", row)
	return 0
}

type articleFlags struct {
	title    *string
	author   *string
	id       *string
	created  *string
	body     *string
	bodyFile *string
	state    *string
	socket   *string
	fs       *flag.FlagSet
}

func newArticleFlags(name string, withStore bool, stderr io.Writer) *articleFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := &articleFlags{
		title:    fs.String("title", "", "article title"),
		author:   fs.String("author", "", "article author"),
		id:       fs.String("id", "", "bulletin id"),
		created:  fs.String("created", "", "RFC3339 time"),
		body:     fs.String("body", "", "article body"),
		bodyFile: fs.String("body-file", "", "file to use as the body"),
		fs:       fs,
	}
	if withStore {
		out.state = fs.String("state", defaultState, "state directory")
		out.socket = fs.String("socket", "", "unix socket path")
	}
	return out
}

func (a *articleFlags) text(stderr io.Writer) (string, int) {
	if *a.body != "" && *a.bodyFile != "" {
		fmt.Fprintln(stderr, "crossbeam: use either --body or --body-file")
		return "", 2
	}
	if *a.bodyFile == "" {
		return *a.body, 0
	}
	file, err := os.Open(*a.bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return "", 1
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, article.MaxBodyBytes+1))
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return "", 1
	}
	return string(raw), 0
}

func cmdPreview(args []string, stdout, stderr io.Writer) int {
	flags := newArticleFlags("preview", false, stderr)
	if err := flags.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: article preview does not take arguments")
		return 2
	}
	body, code := flags.text(stderr)
	if code != 0 {
		return code
	}
	created, err := store.ParseCreated(*flags.created, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	item := article.Article{ID: *flags.id, Title: *flags.title, Author: *flags.author, Created: created, Body: body}
	packet, err := item.RenderPacket()
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	micron, err := item.RenderMicron()
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "--- packet ---\n%s--- micron ---\n%s", packet, micron)
	return 0
}

func cmdSave(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := newArticleFlags("save", true, stderr)
	if err := flags.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: article save does not take arguments")
		return 2
	}
	body, code := flags.text(stderr)
	if code != 0 {
		return code
	}
	back, code := openBackend(flags.fs, *flags.state, *flags.socket, stderr)
	if code != 0 {
		return code
	}
	defer back.close()
	in := store.ArticleIn{
		ID: *flags.id, Title: *flags.title, Author: *flags.author,
		Created: *flags.created, Body: body, Now: time.Now(),
	}
	rowID, err := back.save(ctx, in)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "saved %d\n", rowID)
	return 0
}

func cmdList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: article list does not take arguments")
		return 2
	}
	back, code := openBackend(fs, *state, *socket, stderr)
	if code != 0 {
		return code
	}
	defer back.close()
	rows, err := back.list(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	for _, row := range rows {
		fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n", row.RowID, row.Created, row.Author, row.Title)
	}
	return 0
}

func splitSub(args []string) (string, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

type backend struct {
	state  string
	st     *store.Store
	client *http.Client
}

func (b *backend) close() {
	if b != nil && b.st != nil {
		b.st.Close()
	}
}

func openBackend(fs *flag.FlagSet, state, socket string, stderr io.Writer) (*backend, int) {
	stateSet, socketSet := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "state":
			stateSet = true
		case "socket":
			socketSet = true
		}
	})
	if socketSet {
		if socket == "" {
			socket = filepath.Join(state, "crossbeam.sock")
		}
		if !unixAlive(socket) {
			fmt.Fprintln(stderr, "crossbeam: socket is not running")
			return nil, 1
		}
		return &backend{state: state, client: socketClient(socket)}, 0
	}
	if !stateSet {
		path := filepath.Join(state, "crossbeam.sock")
		if unixAlive(path) {
			return &backend{state: state, client: socketClient(path)}, 0
		}
	}
	st, err := store.Open(state)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return nil, 1
	}
	return &backend{state: state, st: st}, 0
}

func unixAlive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func socketClient(path string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", path)
			},
		},
	}
}

func (b *backend) settings(ctx context.Context) (map[string]string, error) {
	if b.st != nil {
		return b.st.Settings(ctx)
	}
	res, err := b.do(ctx, http.MethodGet, "/api/settings", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, httpErr(res)
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (b *backend) apply(ctx context.Context, patch map[string]string) (map[string]string, error) {
	if b.st != nil {
		return b.st.Apply(ctx, patch)
	}
	res, err := b.do(ctx, http.MethodPut, "/api/settings", patch)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, httpErr(res)
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

type listedArticle struct {
	RowID   int64
	Title   string
	Author  string
	Created string
}

func (b *backend) save(ctx context.Context, in store.ArticleIn) (int64, error) {
	if b.st != nil {
		saved, err := b.st.SaveArticle(ctx, in)
		if err != nil {
			return 0, err
		}
		return saved.RowID, nil
	}
	res, err := b.do(ctx, http.MethodPost, "/api/articles", map[string]string{
		"id": in.ID, "title": in.Title, "author": in.Author, "created": in.Created, "body": in.Body,
	})
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, httpErr(res)
	}
	var body struct {
		RowID int64 `json:"row_id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return 0, err
	}
	return body.RowID, nil
}

func (b *backend) list(ctx context.Context) ([]listedArticle, error) {
	if b.st != nil {
		rows, err := b.st.ListArticles(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]listedArticle, 0, len(rows))
		for _, row := range rows {
			out = append(out, listedArticle{
				RowID: row.RowID, Title: row.Article.Title, Author: row.Article.Author,
				Created: row.Article.Created.Format(time.RFC3339),
			})
		}
		return out, nil
	}
	res, err := b.do(ctx, http.MethodGet, "/api/articles", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, httpErr(res)
	}
	var body struct {
		Articles []struct {
			RowID   int64  `json:"row_id"`
			Title   string `json:"title"`
			Author  string `json:"author"`
			Created string `json:"created"`
		} `json:"articles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]listedArticle, 0, len(body.Articles))
	for _, row := range body.Articles {
		out = append(out, listedArticle{RowID: row.RowID, Title: row.Title, Author: row.Author, Created: row.Created})
	}
	return out, nil
}

func (b *backend) do(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://crossbeam"+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-Crossbeam-Request", "1")
	}
	return b.client.Do(req)
}

func httpErr(res *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	_ = json.Unmarshal(raw, &body)
	if body.Error == "" {
		return fmt.Errorf("the panel returned %s", res.Status)
	}
	return errors.New(body.Error)
}
