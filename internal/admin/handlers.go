// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
)

type app struct {
	st      *store.Store
	listen  string
	url     string
	sha     string
	version string
	hosts   map[string]struct{}
	lim     *limiter
	tmpl    *template.Template
}

type fieldView struct {
	Key   string
	Help  string
	Value string
	Kind  string
}

type articleView struct {
	RowID   int64
	Title   string
	Author  string
	Created string
	ID      string
}

type page struct {
	Title    string
	Station  string
	Authed   bool
	CSRF     string
	Error    string
	Kind     string
	Version  string
	Fields   []fieldView
	RowID    int64
	ArtTitle string
	Author   string
	ArtID    string
	Created  string
	Body     string
	Packet   string
	Micron   string
	Articles []articleView
}

func (a *app) handler(local bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.serve(w, r, local)
	})
}

func (a *app) serve(w http.ResponseWriter, r *http.Request, local bool) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = path.Clean(r.URL.Path)
	if r.URL.Path == "." {
		r.URL.Path = "/"
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if !local && !hostAllowed(r, a.hosts) {
		http.Error(w, "host is not allowed", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/app.css" && r.Method == http.MethodGet:
		a.static(w, r, "web/app.css", "text/css; charset=utf-8")
	case r.URL.Path == "/app.js" && r.Method == http.MethodGet:
		if _, ok := a.requireSession(w, r, local); !ok {
			return
		}
		a.static(w, r, "web/app.js", "text/javascript; charset=utf-8")
	case r.URL.Path == "/login" && r.Method == http.MethodGet:
		a.loginPage(w, r, local, "")
	case r.URL.Path == "/login" && r.Method == http.MethodPost:
		a.login(w, r, local, false)
	case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
		a.login(w, r, local, true)
	case r.URL.Path == "/logout" && r.Method == http.MethodPost:
		a.logout(w, r, local, false)
	case r.URL.Path == "/api/logout" && r.Method == http.MethodPost:
		a.logout(w, r, local, true)
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		if _, ok := a.session(r); ok || local {
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case r.URL.Path == "/settings" && r.Method == http.MethodGet:
		a.settingsPage(w, r, local, "")
	case r.URL.Path == "/settings" && r.Method == http.MethodPost:
		a.settingsPost(w, r, local)
	case r.URL.Path == "/compose" && r.Method == http.MethodGet:
		a.composePage(w, r, local, page{Kind: "compose", Title: "Compose"})
	case r.URL.Path == "/compose" && r.Method == http.MethodPost:
		a.composePost(w, r, local)
	case r.URL.Path == "/articles" && r.Method == http.MethodGet:
		a.articlesPage(w, r, local)
	case r.URL.Path == "/api/session" && r.Method == http.MethodGet:
		a.sessionStatus(w, r, local)
	case r.URL.Path == "/api/status" && r.Method == http.MethodGet:
		a.status(w, r, local)
	case r.URL.Path == "/api/settings" && r.Method == http.MethodGet:
		a.settingsGet(w, r, local)
	case r.URL.Path == "/api/settings" && r.Method == http.MethodPut:
		a.settingsPut(w, r, local)
	case r.URL.Path == "/api/preview" && r.Method == http.MethodPost:
		a.preview(w, r, local)
	case r.URL.Path == "/api/articles" && r.Method == http.MethodGet:
		a.articlesGet(w, r, local)
	case r.URL.Path == "/api/articles" && r.Method == http.MethodPost:
		a.articlesPost(w, r, local)
	case strings.HasPrefix(r.URL.Path, "/api/articles/") && (r.Method == http.MethodGet || r.Method == http.MethodPut):
		a.articleOne(w, r, local)
	case strings.HasPrefix(r.URL.Path, "/articles/") && r.Method == http.MethodGet:
		a.articlePage(w, r, local)
	default:
		http.NotFound(w, r)
	}
}

func (a *app) static(w http.ResponseWriter, r *http.Request, name, contentType string) {
	b, err := webFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}

func (a *app) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	csrf, err := a.st.LookupSession(r.Context(), c.Value)
	if err != nil {
		return "", false
	}
	return csrf, true
}

func (a *app) requireSession(w http.ResponseWriter, r *http.Request, local bool) (string, bool) {
	if local {
		return "", true
	}
	csrf, ok := a.session(r)
	if !ok {
		a.unauth(w, r)
		return "", false
	}
	return csrf, true
}

func (a *app) requireChange(w http.ResponseWriter, r *http.Request, local, api bool) (string, bool) {
	if !local && !api {
		csrf, ok := a.requireSession(w, r, false)
		if !ok {
			return "", false
		}
		if err := r.ParseForm(); err != nil {
			a.badForm(w, err)
			return "", false
		}
		if !csrfOK(lastValue(r, "csrf"), csrf) {
			http.Error(w, "The form token is missing or wrong.", http.StatusForbidden)
			return "", false
		}
		return csrf, true
	}
	if _, ok := a.requireSession(w, r, local); !ok {
		return "", false
	}
	if r.Header.Get(requestHeader) != "1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "X-Crossbeam-Request is required"})
		return "", false
	}
	return "", true
}

func (a *app) unauth(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/app.js") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Sign in is required."})
		return
	}
	a.render(w, http.StatusUnauthorized, page{
		Title:   "Sign in",
		Station: "Crossbeam",
		Kind:    "login",
		Version: a.version,
		Error:   "Sign in is required.",
	})
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request, local bool, msg string) {
	if _, ok := a.session(r); ok || local {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	a.render(w, http.StatusOK, page{
		Title:   "Sign in",
		Station: "Crossbeam",
		Kind:    "login",
		Version: a.version,
		Error:   msg,
	})
}

func (a *app) login(w http.ResponseWriter, r *http.Request, local, api bool) {
	if local {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The local socket does not use a password."})
		return
	}
	ip := remoteIP(r)
	if a.lim.locked(ip) {
		if api {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many sign-in attempts. Wait a minute and try again."})
			return
		}
		a.render(w, http.StatusTooManyRequests, page{
			Title: "Sign in", Station: "Crossbeam", Kind: "login", Version: a.version,
			Error: "Too many sign-in attempts. Wait a minute and try again.",
		})
		return
	}
	password, err := a.readPassword(r)
	if err != nil {
		if tooLarge(err) {
			http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		if api {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The sign-in request could not be read."})
			return
		}
		a.loginPage(w, r, false, "The sign-in request could not be read.")
		return
	}
	ok, err := a.st.CheckPassword(r.Context(), password)
	if err != nil {
		http.Error(w, "Sign-in is unavailable.", http.StatusInternalServerError)
		return
	}
	if !ok {
		a.lim.fail(ip)
		if api {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": badPassword})
			return
		}
		a.render(w, http.StatusUnauthorized, page{
			Title: "Sign in", Station: "Crossbeam", Kind: "login", Version: a.version,
			Error: badPassword,
		})
		return
	}
	a.lim.success(ip)
	session, err := a.st.CreateSession(r.Context())
	if err != nil {
		http.Error(w, "Sign-in is unavailable.", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    session.Token,
		Path:     "/",
		MaxAge:   int(session.Expires.Sub(time.Now()).Seconds()),
		Expires:  session.Expires,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	if api {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *app) readPassword(r *http.Request) (string, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", err
		}
		return body.Password, nil
	}
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	return lastValue(r, "password"), nil
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, local, api bool) {
	if api || local {
		if _, ok := a.requireChange(w, r, local, true); !ok {
			return
		}
	} else if _, ok := a.requireChange(w, r, false, false); !ok {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.st.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	if api || local {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) settingsPage(w http.ResponseWriter, r *http.Request, local bool, msg string) {
	csrf, ok := a.requireSession(w, r, local)
	if !ok {
		return
	}
	current, err := a.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "Settings could not be read.", http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if msg != "" {
		status = http.StatusBadRequest
	}
	a.render(w, status, page{
		Title:   "Settings",
		Station: stationName(current),
		Authed:  true,
		CSRF:    csrf,
		Error:   msg,
		Kind:    "settings",
		Version: a.version,
		Fields:  fieldViews(current),
	})
}

func (a *app) settingsPost(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireChange(w, r, local, false); !ok {
		return
	}
	patch := map[string]string{}
	for key, values := range r.PostForm {
		if key == "csrf" || len(values) == 0 {
			continue
		}
		patch[key] = values[len(values)-1]
	}
	if _, err := a.st.Apply(r.Context(), patch); err != nil {
		a.settingsPage(w, r, local, cleanErr(err))
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *app) settingsGet(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	current, err := a.st.Settings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Settings could not be read."})
		return
	}
	writeJSON(w, http.StatusOK, current)
}

func (a *app) settingsPut(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireChange(w, r, local, true); !ok {
		return
	}
	patch, err := readStringMap(r)
	if err != nil {
		writeJSON(w, statusOf(err), map[string]string{"error": "The settings could not be read."})
		return
	}
	next, err := a.st.Apply(r.Context(), patch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": cleanErr(err)})
		return
	}
	writeJSON(w, http.StatusOK, next)
}

func (a *app) composePage(w http.ResponseWriter, r *http.Request, local bool, view page) {
	csrf, ok := a.requireSession(w, r, local)
	if !ok {
		return
	}
	current, err := a.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "Settings could not be read.", http.StatusInternalServerError)
		return
	}
	view.Station = stationName(current)
	view.Authed = true
	view.CSRF = csrf
	view.Kind = "compose"
	view.Version = a.version
	if view.Title == "" {
		view.Title = "Compose"
	}
	status := http.StatusOK
	if view.Error != "" {
		status = http.StatusBadRequest
	}
	a.render(w, status, view)
}

func (a *app) composePost(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireChange(w, r, local, false); !ok {
		return
	}
	view := page{
		ArtTitle: lastValue(r, "title"),
		Author:   lastValue(r, "author"),
		ArtID:    lastValue(r, "id"),
		Created:  lastValue(r, "created"),
		Body:     lastValue(r, "body"),
	}
	if raw := lastValue(r, "row_id"); raw != "" && raw != "0" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			view.Error = "The article row is not valid."
			a.composePage(w, r, local, view)
			return
		}
		view.RowID = id
	}
	in := store.ArticleIn{
		RowID:   view.RowID,
		ID:      view.ArtID,
		Title:   view.ArtTitle,
		Author:  view.Author,
		Created: view.Created,
		Body:    view.Body,
		Now:     time.Now(),
	}
	if lastValue(r, "action") == "save" {
		saved, err := a.st.SaveArticle(r.Context(), in)
		if err != nil {
			view.Error = cleanErr(err)
			a.composePage(w, r, local, view)
			return
		}
		http.Redirect(w, r, "/articles/"+strconv.FormatInt(saved.RowID, 10), http.StatusSeeOther)
		return
	}
	preview, packet, micron, err := a.renderArticle(r, in)
	if err != nil {
		view.Error = cleanErr(err)
		a.composePage(w, r, local, view)
		return
	}
	view.ArtTitle = preview.Title
	view.Author = preview.Author
	view.ArtID = preview.ID
	view.Created = preview.Created.Format(time.RFC3339)
	view.Body = preview.Body
	view.Packet = packet
	view.Micron = micron
	a.composePage(w, r, local, view)
}

func (a *app) articlesPage(w http.ResponseWriter, r *http.Request, local bool) {
	csrf, ok := a.requireSession(w, r, local)
	if !ok {
		return
	}
	current, err := a.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "Settings could not be read.", http.StatusInternalServerError)
		return
	}
	list, err := a.st.ListArticles(r.Context())
	if err != nil {
		http.Error(w, cleanErr(err), http.StatusInternalServerError)
		return
	}
	view := page{
		Title: "Articles", Station: stationName(current), Authed: true, CSRF: csrf,
		Kind: "articles", Version: a.version,
	}
	for _, item := range list {
		view.Articles = append(view.Articles, articleView{
			RowID: item.RowID, Title: item.Article.Title, Author: item.Article.Author,
			Created: item.Article.Created.Format(time.RFC3339), ID: item.Article.ID,
		})
	}
	a.render(w, http.StatusOK, view)
}

func (a *app) articlePage(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	rowID, err := rowFromPath(r.URL.Path, "/articles/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	saved, err := a.st.Article(r.Context(), rowID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, cleanErr(err), http.StatusInternalServerError)
		return
	}
	packet, err := saved.Article.RenderPacket()
	if err != nil {
		http.Error(w, cleanErr(err), http.StatusInternalServerError)
		return
	}
	micron, err := saved.Article.RenderMicron()
	if err != nil {
		http.Error(w, cleanErr(err), http.StatusInternalServerError)
		return
	}
	a.composePage(w, r, local, page{
		Title: "Article", RowID: saved.RowID, ArtTitle: saved.Article.Title,
		Author: saved.Article.Author, ArtID: saved.Article.ID,
		Created: saved.Article.Created.Format(time.RFC3339), Body: saved.Article.Body,
		Packet: packet, Micron: micron,
	})
}

func (a *app) preview(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireChange(w, r, local, true); !ok {
		return
	}
	in, err := a.articleIn(r, 0)
	if err != nil {
		writeJSON(w, statusOf(err), map[string]string{"error": "The article could not be read."})
		return
	}
	preview, packet, micron, err := a.renderArticle(r, in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": cleanErr(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"created": preview.Created.Format(time.RFC3339),
		"packet":  packet,
		"micron":  micron,
	})
}

func (a *app) articlesPost(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireChange(w, r, local, true); !ok {
		return
	}
	in, err := a.articleIn(r, 0)
	if err != nil {
		writeJSON(w, statusOf(err), map[string]string{"error": "The article could not be read."})
		return
	}
	a.writeSaved(w, r, in)
}

func (a *app) articlesGet(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	list, err := a.st.ListArticles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": cleanErr(err)})
		return
	}
	type row struct {
		RowID   int64  `json:"row_id"`
		ID      string `json:"id"`
		Title   string `json:"title"`
		Author  string `json:"author"`
		Created string `json:"created"`
	}
	out := make([]row, 0, len(list))
	for _, item := range list {
		out = append(out, row{
			RowID: item.RowID, ID: item.Article.ID, Title: item.Article.Title,
			Author: item.Article.Author, Created: item.Article.Created.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": out})
}

func (a *app) articleOne(w http.ResponseWriter, r *http.Request, local bool) {
	rowID, err := rowFromPath(r.URL.Path, "/api/articles/")
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	if r.Method == http.MethodPut {
		if _, ok := a.requireChange(w, r, local, true); !ok {
			return
		}
		in, err := a.articleIn(r, rowID)
		if err != nil {
			writeJSON(w, statusOf(err), map[string]string{"error": "The article could not be read."})
			return
		}
		a.writeSaved(w, r, in)
		return
	}
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	saved, err := a.st.Article(r.Context(), rowID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": cleanErr(err)})
		return
	}
	packet, micron, err := renderPair(saved.Article)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": cleanErr(err)})
		return
	}
	writeJSON(w, http.StatusOK, savedJSON(saved, packet, micron))
}

func (a *app) writeSaved(w http.ResponseWriter, r *http.Request, in store.ArticleIn) {
	saved, err := a.st.SaveArticle(r.Context(), in)
	if errors.Is(err, store.ErrDuplicateID{}) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": cleanErr(err)})
		return
	}
	packet, micron, err := renderPair(saved.Article)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": cleanErr(err)})
		return
	}
	writeJSON(w, http.StatusOK, savedJSON(saved, packet, micron))
}

func (a *app) status(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	current, err := a.st.Settings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Settings could not be read."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     a.version,
		"listen":      a.listen,
		"public":      current[settings.KeyPublic] == "true",
		"url":         a.url,
		"cert_sha256": a.sha,
	})
}

func (a *app) sessionStatus(w http.ResponseWriter, r *http.Request, local bool) {
	if _, ok := a.requireSession(w, r, local); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) renderArticle(r *http.Request, in store.ArticleIn) (article.Article, string, string, error) {
	prepared, err := a.st.Prepare(r.Context(), in)
	if err != nil {
		return article.Article{}, "", "", err
	}
	packet, micron, err := renderPair(prepared)
	if err != nil {
		return article.Article{}, "", "", err
	}
	return prepared, packet, micron, nil
}

func (a *app) articleIn(r *http.Request, rowID int64) (store.ArticleIn, error) {
	var body struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Author  string `json:"author"`
		Created string `json:"created"`
		Body    string `json:"body"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		return store.ArticleIn{}, err
	}
	return store.ArticleIn{
		RowID: rowID, ID: body.ID, Title: body.Title, Author: body.Author,
		Created: body.Created, Body: body.Body, Now: time.Now(),
	}, nil
}

func (a *app) render(w http.ResponseWriter, status int, view page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.ExecuteTemplate(w, "page", view)
}

func (a *app) badForm(w http.ResponseWriter, err error) {
	if tooLarge(err) {
		http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "The form could not be read.", http.StatusBadRequest)
}

func renderPair(item article.Article) (string, string, error) {
	packet, err := item.RenderPacket()
	if err != nil {
		return "", "", err
	}
	micron, err := item.RenderMicron()
	if err != nil {
		return "", "", err
	}
	return packet, micron, nil
}

func savedJSON(saved store.SavedArticle, packet, micron string) map[string]any {
	return map[string]any{
		"row_id":  saved.RowID,
		"id":      saved.Article.ID,
		"title":   saved.Article.Title,
		"author":  saved.Article.Author,
		"created": saved.Article.Created.Format(time.RFC3339),
		"body":    saved.Article.Body,
		"packet":  packet,
		"micron":  micron,
	}
}

func fieldViews(current map[string]string) []fieldView {
	out := make([]fieldView, 0, len(settings.Schema))
	for _, field := range settings.Schema {
		kind := "line"
		switch field.Kind {
		case settings.KindBool:
			kind = "bool"
		case settings.KindListen:
			kind = "listen"
		}
		out = append(out, fieldView{Key: field.Key, Help: field.Help, Value: current[field.Key], Kind: kind})
	}
	return out
}

func stationName(current map[string]string) string {
	if current[settings.KeyName] == "" {
		return "Crossbeam"
	}
	return current[settings.KeyName]
}

func lastValue(r *http.Request, name string) string {
	values := r.PostForm[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func rowFromPath(path, prefix string) (int64, error) {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" || strings.Contains(raw, "/") {
		return 0, errors.New("not found")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("not found")
	}
	return id, nil
}

func readStringMap(r *http.Request) (map[string]string, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody+1))
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, errors.New("settings must be strings")
		}
		out[key] = text
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 300 || strings.Contains(text, "\n") {
		return "The change was rejected."
	}
	return text
}

func tooLarge(err error) bool {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "request body too large")
}

func statusOf(err error) int {
	if tooLarge(err) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
