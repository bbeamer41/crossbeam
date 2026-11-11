// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package fcc checks an amateur callsign against the FCC Universal
// Licensing System. A callsign may be approved only when the record is
// an active, unexpired amateur license. The check fails closed when the
// FCC cannot be read. Licensee names, addresses, and FRNs are not kept.
package fcc

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	userAgent = "Crossbeam amateur license check"
	apiURL    = "https://www.fcc.gov/api/license-view/basicSearch/getLicenses"
	weeklyURL = "https://data.fcc.gov/download/pub/uls/complete/l_amat.zip"
	dailyURL  = "https://data.fcc.gov/download/pub/uls/daily/l_am_%s.zip"

	apiWait   = 5 * time.Second
	apiBytes  = 1 << 20
	weeklyMax = 240 << 20
	dailyMax  = 8 << 20
	hdMax     = 800 << 20
	maxGrants = 2000000
	lineMax   = 64 << 10
)

var errLookup = errors.New("fcc license lookup failed")

var days = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Client reads the FCC license-view query and, when that query does not
// return a license document, the published amateur database.
type Client struct {
	// API is the license-view getLicenses URL. Empty skips that query.
	API string
	// Weekly is the complete amateur license zip.
	Weekly string
	// Daily is a format string with one %s for sun, mon, tue, wed, thu,
	// fri, or sat. Empty skips the daily files.
	Daily string
	HTTP  *http.Client
	// Now is the clock used for the expiration date. Nil uses time.Now.
	Now func() time.Time

	mu      sync.Mutex
	loc     *time.Location
	snap    *snapshot
	loading chan struct{}
}

type snapshot struct {
	stamp stamp
	exp   map[string]uint32
}

type stamp struct {
	weekly time.Time
	daily  [7]time.Time
}

func (s stamp) same(o stamp) bool {
	if !s.weekly.Equal(o.weekly) {
		return false
	}
	for i := range s.daily {
		if !s.daily[i].Equal(o.daily[i]) {
			return false
		}
	}
	return true
}

// New is the client used when the radio is on.
func New() *Client {
	return &Client{
		API:    apiURL,
		Weekly: weeklyURL,
		Daily:  dailyURL,
		HTTP: &http.Client{
			Timeout: 3 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 || len(via) >= 3 || !sameSite(via[0].URL, req.URL) {
					return errLookup
				}
				if fccHost(req.URL.Host) && req.URL.Scheme != "https" {
					return errLookup
				}
				return nil
			},
		},
	}
}

// Active reports whether call is an active, unexpired FCC amateur license.
// call may include an SSID. A false result is a completed check. An error
// means the FCC could not be read.
func (c *Client) Active(ctx context.Context, call string) (bool, error) {
	if c == nil {
		return false, errLookup
	}
	base, ok := BaseCall(call)
	if !ok {
		return false, nil
	}
	today, err := c.today()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(c.API) != "" {
		decided, allow, apiErr := c.askAPI(ctx, base, today)
		if decided {
			return allow, nil
		}
		if apiErr != nil && strings.TrimSpace(c.Weekly) == "" {
			return false, errLookup
		}
	}
	if strings.TrimSpace(c.Weekly) == "" {
		return false, errLookup
	}
	if err := c.ensure(ctx); err != nil {
		return false, errLookup
	}
	return c.allowed(base, today), nil
}

// BaseCall strips an SSID and returns an AX.25 callsign.
func BaseCall(call string) (string, bool) {
	call = strings.ToUpper(strings.TrimSpace(call))
	if i := strings.IndexByte(call, '-'); i >= 0 {
		call = call[:i]
	}
	if len(call) < 1 || len(call) > 6 {
		return "", false
	}
	for i := 0; i < len(call); i++ {
		c := call[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "", false
		}
	}
	return call, true
}

func (c *Client) askAPI(ctx context.Context, call string, today uint32) (bool, bool, error) {
	apiCtx, cancel := context.WithTimeout(ctx, apiWait)
	defer cancel()
	u, err := url.Parse(strings.TrimSpace(c.API))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false, false, errLookup
	}
	if fccHost(u.Host) && u.Scheme != "https" {
		return false, false, errLookup
	}
	q := u.Query()
	q.Set("searchValue", call)
	q.Set("format", "json")
	q.Set("pageSize", "100")
	u.RawQuery = q.Encode()
	resp, err := c.round(apiCtx, http.MethodGet, u.String(), "application/json")
	if err != nil {
		return false, false, errLookup
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return false, false, errLookup
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiBytes+1))
	if err != nil || len(body) > apiBytes {
		return false, false, errLookup
	}
	allow, ok := decideJSON(body, call, today)
	if !ok {
		return false, false, errLookup
	}
	return true, allow, nil
}

func decideJSON(body []byte, call string, today uint32) (bool, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return false, false
	}
	raw, ok := top["Licenses"]
	if !ok || string(raw) == "null" {
		return false, false
	}
	var doc struct {
		License json.RawMessage `json:"License"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false, false
	}
	if len(doc.License) == 0 || string(doc.License) == "null" {
		return false, true
	}
	var rows []licenseRow
	if doc.License[0] == '[' {
		if json.Unmarshal(doc.License, &rows) != nil {
			return false, false
		}
	} else {
		var one licenseRow
		if json.Unmarshal(doc.License, &one) != nil {
			return false, false
		}
		rows = []licenseRow{one}
	}
	for _, row := range rows {
		if licenseAllows(row, call, today) {
			return true, true
		}
	}
	return false, true
}

type licenseRow struct {
	Callsign         string `json:"callsign"`
	ServiceDesc      string `json:"serviceDesc"`
	StatusDesc       string `json:"statusDesc"`
	ExpiredDate      string `json:"expiredDate"`
	ServiceCode      string `json:"serviceCode"`
	RadioServiceCode string `json:"radioServiceCode"`
	StatusCode       string `json:"statusCode"`
	LicStatusCode    string `json:"licStatusCode"`
	CancellationDate string `json:"cancellationDate"`
}

func licenseAllows(row licenseRow, call string, today uint32) bool {
	got, ok := BaseCall(row.Callsign)
	if !ok || got != call {
		return false
	}
	if strings.TrimSpace(row.CancellationDate) != "" {
		return false
	}
	code := firstText(row.ServiceCode, row.RadioServiceCode)
	if !serviceAmateur(code, row.ServiceDesc) {
		return false
	}
	status := firstText(row.StatusCode, row.LicStatusCode)
	if !statusActive(status, row.StatusDesc) {
		return false
	}
	exp, ok := parseDate(row.ExpiredDate)
	return ok && exp >= today
}

func firstText(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func serviceAmateur(code, desc string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch code {
	case "HA", "HV":
		return true
	case "":
	default:
		return false
	}
	desc = strings.ToUpper(strings.TrimSpace(desc))
	return desc == "AMATEUR" || desc == "AMATEUR VANITY"
}

func statusActive(code, desc string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	desc = strings.ToUpper(strings.TrimSpace(desc))
	if code == "" && desc == "" {
		return false
	}
	if code != "" && code != "A" {
		return false
	}
	if desc != "" && desc != "ACTIVE" {
		return false
	}
	return true
}

func (c *Client) ensure(ctx context.Context) error {
	for {
		next, err := c.publication(ctx)
		if err != nil {
			return err
		}
		c.mu.Lock()
		if c.snap != nil && c.snap.stamp.same(next) {
			c.mu.Unlock()
			return nil
		}
		wait := c.loading
		if wait == nil {
			wait = make(chan struct{})
			c.loading = wait
			c.mu.Unlock()
			err = c.reload(ctx, next)
			c.mu.Lock()
			c.loading = nil
			c.mu.Unlock()
			close(wait)
			return err
		}
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return errLookup
		}
	}
}

func (c *Client) publication(ctx context.Context) (stamp, error) {
	var out stamp
	mod, length, code, err := c.stat(ctx, c.Weekly)
	if err != nil || code != http.StatusOK || length > weeklyMax {
		return stamp{}, errLookup
	}
	out.weekly = mod
	if strings.TrimSpace(c.Daily) == "" {
		return out, nil
	}
	for i, day := range days {
		raw := fmtDaily(c.Daily, day)
		mod, length, code, err = c.stat(ctx, raw)
		if err != nil {
			return stamp{}, errLookup
		}
		if code == http.StatusNotFound {
			continue
		}
		if code != http.StatusOK || length > dailyMax {
			return stamp{}, errLookup
		}
		if mod.After(out.weekly) {
			out.daily[i] = mod
		}
	}
	return out, nil
}

func fmtDaily(pattern, day string) string {
	if strings.Contains(pattern, "%s") {
		return strings.Replace(pattern, "%s", day, 1)
	}
	return pattern
}

func (c *Client) reload(ctx context.Context, want stamp) error {
	// Another waiter may have loaded this publication while we waited
	// to start. The caller also checks, but a single loader must not
	// download twice.
	c.mu.Lock()
	if c.snap != nil && c.snap.stamp.same(want) {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	grants := make(map[string]uint32)
	if err := c.readZip(ctx, c.Weekly, weeklyMax, grants, false); err != nil {
		return err
	}
	var newer []timedURL
	for i, at := range want.daily {
		if at.IsZero() {
			continue
		}
		newer = append(newer, timedURL{at: at, url: fmtDaily(c.Daily, days[i])})
	}
	for i := 1; i < len(newer); i++ {
		item := newer[i]
		j := i
		for j > 0 && newer[j-1].at.After(item.at) {
			newer[j] = newer[j-1]
			j--
		}
		newer[j] = item
	}
	for _, file := range newer {
		if err := c.readZip(ctx, file.url, dailyMax, grants, true); err != nil {
			return err
		}
	}
	if len(grants) > maxGrants {
		return errLookup
	}
	c.mu.Lock()
	c.snap = &snapshot{stamp: want, exp: grants}
	c.mu.Unlock()
	return nil
}

type timedURL struct {
	at  time.Time
	url string
}

func (c *Client) readZip(ctx context.Context, raw string, limit int64, grants map[string]uint32, replace bool) error {
	path, err := c.download(ctx, raw, limit)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	zr, err := zip.OpenReader(path)
	if err != nil {
		return errLookup
	}
	defer zr.Close()
	var hd *zip.File
	for _, file := range zr.File {
		if strings.EqualFold(file.Name, "HD.dat") {
			hd = file
			break
		}
	}
	if hd == nil || hd.UncompressedSize64 > hdMax {
		return errLookup
	}
	rc, err := hd.Open()
	if err != nil {
		return errLookup
	}
	defer rc.Close()
	rows, err := readHD(rc, grants, replace)
	if err != nil {
		return err
	}
	if !replace && rows == 0 {
		return errLookup
	}
	return nil
}

func (c *Client) allowed(call string, today uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil {
		return false
	}
	exp, ok := c.snap.exp[call]
	return ok && exp >= today
}

func (c *Client) today() (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loc == nil {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			return 0, errLookup
		}
		c.loc = loc
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	year, month, day := now.In(c.loc).Date()
	return uint32(year*10000 + int(month)*100 + day), nil
}

func (c *Client) stat(ctx context.Context, raw string) (time.Time, int64, int, error) {
	resp, err := c.round(ctx, http.MethodHead, raw, "")
	if err != nil {
		return time.Time{}, 0, 0, errLookup
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNotFound {
		return time.Time{}, 0, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, 0, resp.StatusCode, errLookup
	}
	mod, ok := lastModified(resp)
	if !ok {
		return time.Time{}, 0, resp.StatusCode, errLookup
	}
	return mod, resp.ContentLength, resp.StatusCode, nil
}

func (c *Client) download(ctx context.Context, raw string, limit int64) (string, error) {
	resp, err := c.round(ctx, http.MethodGet, raw, "application/zip")
	if err != nil {
		return "", errLookup
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", errLookup
	}
	f, err := os.CreateTemp("", "crossbeam-fcc-")
	if err != nil {
		return "", errLookup
	}
	name := f.Name()
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n > limit {
		os.Remove(name)
		return "", errLookup
	}
	return name, nil
}

func (c *Client) round(ctx context.Context, method, raw, accept string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errLookup
	}
	if fccHost(u.Host) && u.Scheme != "https" {
		return nil, errLookup
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, errLookup
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, errLookup
	}
	return resp, nil
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func lastModified(resp *http.Response) (time.Time, bool) {
	text := resp.Header.Get("Last-Modified")
	if text == "" {
		return time.Time{}, false
	}
	mod, err := http.ParseTime(text)
	if err != nil {
		return time.Time{}, false
	}
	return mod, true
}

func fccHost(host string) bool {
	name := host
	if h, _, err := splitHost(host); err == nil {
		name = h
	}
	name = strings.ToLower(name)
	return name == "fcc.gov" || name == "www.fcc.gov" || name == "data.fcc.gov"
}

func splitHost(host string) (string, string, error) {
	if strings.HasPrefix(host, "[") {
		end := strings.LastIndex(host, "]")
		if end < 0 {
			return "", "", errLookup
		}
		return host[:end+1], "", nil
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i], host[i+1:], nil
	}
	return host, "", nil
}

func sameSite(origin, next *url.URL) bool {
	if origin == nil || next == nil {
		return false
	}
	if strings.EqualFold(origin.Host, next.Host) {
		return true
	}
	return fccHost(origin.Host) && fccHost(next.Host)
}

func readHD(r io.Reader, grants map[string]uint32, replace bool) (int, error) {
	limited := &io.LimitedReader{R: r, N: hdMax + 1}
	br := bufio.NewReaderSize(limited, lineMax)
	rows := 0
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 && bytesHasHD(line) {
			if len(line) > 4096 {
				return rows, errLookup
			}
			rows++
			if err := applyHD(line, grants, replace); err != nil {
				return rows, err
			}
		} else if len(line) >= lineMax && err == bufio.ErrBufferFull {
			return rows, errLookup
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return rows, errLookup
		}
	}
	if limited.N <= 0 {
		return rows, errLookup
	}
	return rows, nil
}

func bytesHasHD(line []byte) bool {
	return len(line) >= 3 && line[0] == 'H' && line[1] == 'D' && line[2] == '|'
}

func applyHD(line []byte, grants map[string]uint32, replace bool) error {
	call, ok := field(line, 5)
	if !ok {
		return nil
	}
	base, ok := BaseCall(call)
	if !ok {
		return nil
	}
	status, ok1 := field(line, 6)
	service, ok2 := field(line, 7)
	expired, ok3 := field(line, 9)
	cancelled, ok4 := field(line, 10)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil
	}
	exp, good := grantDate(status, service, expired, cancelled)
	if !good {
		if replace {
			delete(grants, base)
		}
		return nil
	}
	if _, exists := grants[base]; !exists && len(grants) >= maxGrants {
		return errLookup
	}
	grants[base] = exp
	return nil
}

func grantDate(status, service, expired, cancelled string) (uint32, bool) {
	if strings.TrimSpace(cancelled) != "" {
		return 0, false
	}
	if strings.ToUpper(strings.TrimSpace(status)) != "A" {
		return 0, false
	}
	svc := strings.ToUpper(strings.TrimSpace(service))
	if svc != "HA" && svc != "HV" {
		return 0, false
	}
	return parseDate(expired)
}

func field(line []byte, want int) (string, bool) {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	start := 0
	n := 1
	for i := 0; i <= len(line); i++ {
		if i == len(line) || line[i] == '|' {
			if n == want {
				return string(line[start:i]), true
			}
			n++
			start = i + 1
			if n > 10 && want <= 10 {
				break
			}
		}
	}
	return "", false
}

func parseDate(text string) (uint32, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	var year, month, day int
	var err error
	if strings.Contains(text, "/") {
		part := strings.Split(text, "/")
		if len(part) != 3 {
			return 0, false
		}
		month, err = strconv.Atoi(part[0])
		if err != nil {
			return 0, false
		}
		day, err = strconv.Atoi(part[1])
		if err != nil {
			return 0, false
		}
		year, err = strconv.Atoi(part[2])
		if err != nil {
			return 0, false
		}
	} else if strings.Contains(text, "-") {
		part := strings.Split(text, "-")
		if len(part) != 3 {
			return 0, false
		}
		year, err = strconv.Atoi(part[0])
		if err != nil {
			return 0, false
		}
		month, err = strconv.Atoi(part[1])
		if err != nil {
			return 0, false
		}
		day, err = strconv.Atoi(part[2])
		if err != nil {
			return 0, false
		}
	} else {
		return 0, false
	}
	if year < 1900 || year > 2200 || month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, false
	}
	when := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if when.Year() != year || int(when.Month()) != month || when.Day() != day {
		return 0, false
	}
	return uint32(year*10000 + month*100 + day), true
}
