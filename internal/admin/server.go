// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package admin is the sysop panel. TCP is HTTPS only. The local command
// uses the same handlers over a Unix socket, without TLS.
package admin

import (
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

//go:embed web
var webFS embed.FS

const maxBody = 1 << 20

// Config chooses how this process listens. Listen overrides the saved
// admin.listen for this start only. SocketPath overrides the socket under
// the state directory. SocketMode is 0600 or 0660; zero means 0600.
type Config struct {
	Listen     string
	SocketPath string
	SocketMode os.FileMode
}

// Running is a live admin panel.
type Running struct {
	URL        string
	Listen     string
	SocketPath string
	CertSHA256 string
	https      *http.Server
	plain      *http.Server
	done       chan error
	stopOnce   sync.Once
}

var pageTmpl = template.Must(template.ParseFS(webFS, "web/templates.html"))

// Start listens on TLS and on the Unix socket. The store stays open;
// the caller closes it after Running stops. Start fails when no sysop
// password is set, or when the address is public and admin.public is false.
func Start(ctx context.Context, st *store.Store, cfg Config) (*Running, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mode, err := socketMode(cfg.SocketMode)
	if err != nil {
		return nil, err
	}
	current, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	public := current[settings.KeyPublic] == "true"
	var addr string
	if cfg.Listen != "" {
		addr, err = settings.ResolveBind(cfg.Listen, public, nil)
	} else {
		addr, err = settings.Resolve(current[settings.KeyListen], public, nil)
	}
	if err != nil {
		return nil, err
	}
	has, err := st.HasPassword(ctx)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, store.ErrNoPassword
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	pair, sha, err := EnsureCert(st.Dir(), host)
	if err != nil {
		return nil, err
	}
	tcpLn, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			tcpLn.Close()
		}
	}()
	bound := tcpLn.Addr().String()
	tlsLn := tls.NewListener(tcpLn, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{pair},
	})
	sock := cfg.SocketPath
	if sock == "" {
		sock = filepath.Join(st.Dir(), "crossbeam.sock")
	}
	unixLn, err := listenUnix(sock, mode)
	if err != nil {
		return nil, err
	}
	app := &app{
		st:      st,
		listen:  bound,
		url:     "https://" + bound,
		sha:     sha,
		version: version.String,
		hosts:   allowedHosts(bound),
		lim:     newLimiter(),
		tmpl:    pageTmpl,
	}
	httpsSrv := newServer(app.handler(false))
	plainSrv := newServer(app.handler(true))
	running := &Running{
		URL:        app.url,
		Listen:     bound,
		SocketPath: sock,
		CertSHA256: sha,
		https:      httpsSrv,
		plain:      plainSrv,
		done:       make(chan error, 1),
	}
	go running.serve(tlsLn, unixLn)
	ok = true
	return running, nil
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (r *Running) serve(tlsLn, unixLn net.Listener) {
	errCh := make(chan error, 2)
	go func() { errCh <- r.https.Serve(tlsLn) }()
	go func() { errCh <- r.plain.Serve(unixLn) }()
	var first error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) && first == nil {
			first = err
		}
		r.Close()
	}
	r.done <- first
}

// Close stops both listeners. It is safe to call more than once.
func (r *Running) Close() error {
	r.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if r.https != nil {
			_ = r.https.Shutdown(ctx)
		}
		if r.plain != nil {
			_ = r.plain.Shutdown(ctx)
		}
		if r.SocketPath != "" {
			_ = os.Remove(r.SocketPath)
		}
	})
	return nil
}

// Wait returns when both listeners have stopped. A clean shutdown is
// not an error.
func (r *Running) Wait() error {
	return <-r.done
}

func socketMode(mode os.FileMode) (os.FileMode, error) {
	if mode == 0 {
		return 0o600, nil
	}
	perm := mode.Perm()
	if perm != 0o600 && perm != 0o660 {
		return 0, errors.New("socket mode must be 0600 or 0660")
	}
	return perm, nil
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
		return nil, err
	}
	if got.Mode().Perm() != mode {
		ln.Close()
		_ = os.Remove(path)
		return nil, errors.New("socket mode was not applied")
	}
	return ln, nil
}

// ParseSocketMode accepts 0600 or 0660.
func ParseSocketMode(text string) (fs.FileMode, error) {
	switch text {
	case "0600":
		return 0o600, nil
	case "0660":
		return 0o660, nil
	default:
		return 0, errors.New("socket mode must be 0600 or 0660")
	}
}
