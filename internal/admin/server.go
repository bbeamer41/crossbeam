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
	"strings"
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
// Lookup resolves a hostname once at start. Nil uses the system resolver.
type Config struct {
	Listen     string
	SocketPath string
	SocketMode os.FileMode
	Lookup     settings.LookupIP
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
	var plan settings.Plan
	if cfg.Listen != "" {
		plan, err = settings.ResolveBind(cfg.Listen, public, cfg.Lookup)
	} else {
		plan, err = settings.Resolve(current[settings.KeyListen], public, cfg.Lookup)
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
	pair, sha, err := EnsureCert(st.Dir(), certHosts(plan)...)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{pair},
	}
	var tlsLns []net.Listener
	var bound []string
	ok := false
	defer func() {
		if !ok {
			for _, ln := range tlsLns {
				_ = ln.Close()
			}
		}
	}()
	for _, addr := range plan.Addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		bound = append(bound, ln.Addr().String())
		tlsLns = append(tlsLns, tls.NewListener(ln, tlsCfg))
	}
	sock := cfg.SocketPath
	if sock == "" {
		sock = filepath.Join(st.Dir(), "crossbeam.sock")
	}
	unixLn, err := listenUnix(sock, mode)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !ok {
			_ = unixLn.Close()
			_ = os.Remove(sock)
		}
	}()
	listenText := strings.Join(bound, ", ")
	app := &app{
		st:      st,
		listen:  listenText,
		url:     panelURL(plan.Name, bound),
		sha:     sha,
		version: version.String,
		hosts:   accessHosts(plan.Name, bound),
		lim:     newLimiter(),
		tmpl:    pageTmpl,
	}
	httpsSrv := newServer(app.handler(false))
	plainSrv := newServer(app.handler(true))
	running := &Running{
		URL:        app.url,
		Listen:     listenText,
		SocketPath: sock,
		CertSHA256: sha,
		https:      httpsSrv,
		plain:      plainSrv,
		done:       make(chan error, 1),
	}
	go running.serve(httpsSrv, tlsLns, plainSrv, unixLn)
	ok = true
	return running, nil
}

func certHosts(plan settings.Plan) []string {
	var hosts []string
	if plan.Name != "" {
		hosts = append(hosts, plan.Name)
	}
	unspecified := false
	for _, addr := range plan.Addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			unspecified = true
			continue
		}
		hosts = append(hosts, host)
	}
	if unspecified {
		hosts = append(hosts, concreteIPs()...)
	}
	if len(hosts) == 0 {
		hosts = []string{"localhost"}
	}
	return hosts
}

func concreteIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP == nil || ipnet.IP.IsUnspecified() {
				continue
			}
			ip := ipnet.IP
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			out = append(out, ip.String())
		}
	}
	return out
}

func panelURL(name string, bound []string) string {
	if len(bound) == 0 {
		return ""
	}
	if name == "" {
		return "https://" + bound[0]
	}
	_, port, err := net.SplitHostPort(bound[0])
	if err != nil {
		return "https://" + bound[0]
	}
	return "https://" + net.JoinHostPort(name, port)
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

func (r *Running) serve(httpsSrv *http.Server, tlsLns []net.Listener, plainSrv *http.Server, unixLn net.Listener) {
	type accept struct {
		srv *http.Server
		ln  net.Listener
	}
	items := make([]accept, 0, len(tlsLns)+1)
	for _, ln := range tlsLns {
		items = append(items, accept{srv: httpsSrv, ln: ln})
	}
	items = append(items, accept{srv: plainSrv, ln: unixLn})
	errCh := make(chan error, len(items))
	for _, item := range items {
		item := item
		go func() { errCh <- item.srv.Serve(item.ln) }()
	}
	var first error
	for range items {
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
