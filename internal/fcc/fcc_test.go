// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package fcc

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIAllowsAnActiveAmateurCall(t *testing.T) {
	const secret = "SECRET NAME 0009999999"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			http.Error(w, secret, http.StatusBadRequest)
			return
		}
		switch r.URL.Query().Get("searchValue") {
		case "K5LEB":
			fmt.Fprintf(w, `{"status":"OK","Licenses":{"rowCount":"2","License":[`+
				`{"callsign":"W1AW","serviceDesc":"Amateur","statusDesc":"Active","expiredDate":"01/01/2036","licName":"%s"},`+
				`{"callsign":"K5LEB","serviceDesc":"Amateur","statusDesc":"Active","expiredDate":"11/10/2026","frn":"0009999999","licName":"%s"}`+
				`]}}`, secret, secret)
		default:
			fmt.Fprintf(w, `{"Licenses":{"License":{"callsign":"W1AW","serviceDesc":"Amateur","statusDesc":"Active","expiredDate":"01/01/2036","licName":"%s"}}}`, secret)
		}
	}))
	defer srv.Close()
	client := testClient(t, srv)
	client.Weekly = ""
	client.Daily = ""
	ok, err := client.Active(t.Context(), "k5leb-1")
	if err != nil || !ok {
		t.Fatalf("allow %v %v", ok, err)
	}
	ok, err = client.Active(t.Context(), "N0CALL")
	if err != nil || ok {
		t.Fatalf("other call %v %v", ok, err)
	}
}

func TestAPIObjectAndRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("searchValue") {
		case "K5LEB":
			io.WriteString(w, `{"Licenses":{"License":{"callsign":"K5LEB","serviceCode":"HV","statusDesc":"Active","expiredDate":"2036-01-02"}}}`)
		case "N0CALL":
			io.WriteString(w, `{"Licenses":{"License":[{"callsign":"N0CALL","serviceDesc":"Amateur","statusDesc":"Expired","expiredDate":"01/01/2020"},{"callsign":"N0CALL","radioServiceCode":"ZA","statusCode":"A","statusDesc":"Active","expiredDate":"01/01/2036"}]}}`)
		case "W1AW":
			io.WriteString(w, `{"Licenses":{"License":{"callsign":"W1AW","serviceDesc":"Amateur","statusDesc":"Active","expiredDate":"01/01/2036","cancellationDate":"01/02/2024"}}}`)
		default:
			io.WriteString(w, `{"Licenses":{"rowCount":0}}`)
		}
	}))
	defer srv.Close()
	client := testClient(t, srv)
	client.Weekly = ""
	if ok, err := client.Active(t.Context(), "K5LEB"); err != nil || !ok {
		t.Fatalf("object %v %v", ok, err)
	}
	if ok, err := client.Active(t.Context(), "N0CALL"); err != nil || ok {
		t.Fatalf("expired %v %v", ok, err)
	}
	if ok, err := client.Active(t.Context(), "W1AW"); err != nil || ok {
		t.Fatalf("cancelled %v %v", ok, err)
	}
	if ok, err := client.Active(t.Context(), "K1JT"); err != nil || ok {
		t.Fatalf("missing %v %v", ok, err)
	}
}

func TestDatabaseWhenTheQueryIsBlocked(t *testing.T) {
	const secret = "SECRET NAME"
	weekly := zipHD(t, ""+
		hd("K5LEB", "A", "HA", "11/10/2036", "")+
		hd("N0CALL", "E", "HA", "01/01/2020", "")+
		hd("W1AW", "A", "HV", "01/01/2036", "01/02/2024")+
		hd("K1JT", "A", "ZA", "01/01/2036", "")+
		"HD|1|||K5LEB|A|HA|01/01/2020|11/10/2036||"+secret+"|\n")
	var weeklyGets atomic.Int32
	weeklyMod := time.Date(2026, 11, 8, 14, 8, 58, 0, time.UTC)
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html>"+secret+"</html>", http.StatusForbidden)
	})
	mux.HandleFunc("/complete/l_amat.zip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			weeklyGets.Add(1)
		}
		http.ServeContent(w, r, "l_amat.zip", weeklyMod, bytes.NewReader(weekly))
	})
	mux.HandleFunc("/daily/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := testClient(t, srv)
	ok, err := client.Active(t.Context(), "K5LEB-1")
	if err != nil || !ok {
		t.Fatalf("licensed %v %v", ok, err)
	}
	if strings.Contains(fmt.Sprint(err), secret) {
		t.Fatal("secret leaked")
	}
	for _, call := range []string{"N0CALL", "W1AW", "K1JT", "NOCALL"} {
		ok, err = client.Active(t.Context(), call)
		if err != nil || ok {
			t.Fatalf("%s %v %v", call, ok, err)
		}
	}
	if _, err := client.Active(t.Context(), "K5LEB"); err != nil {
		t.Fatal(err)
	}
	if weeklyGets.Load() != 1 {
		t.Fatalf("weekly downloads %d", weeklyGets.Load())
	}
}

func TestDailyFileReplacesTheWeeklyRecord(t *testing.T) {
	weekly := zipHD(t, hd("K5LEB", "A", "HA", "11/10/2036", ""))
	daily := zipHD(t, hd("K5LEB", "A", "HA", "11/09/2026", "")+hd("W1AW", "A", "HV", "11/10/2036", ""))
	weeklyMod := time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC)
	dailyMod := time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC)
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	})
	mux.HandleFunc("/complete/l_amat.zip", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "l_amat.zip", weeklyMod, bytes.NewReader(weekly))
	})
	mux.HandleFunc("/daily/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/l_am_mon.zip") {
			http.ServeContent(w, r, "l_am_mon.zip", dailyMod, bytes.NewReader(daily))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := testClient(t, srv)
	if ok, err := client.Active(t.Context(), "K5LEB"); err != nil || ok {
		t.Fatalf("revoked %v %v", ok, err)
	}
	if ok, err := client.Active(t.Context(), "W1AW"); err != nil || !ok {
		t.Fatalf("added %v %v", ok, err)
	}
}

func TestExpirationUsesEasternTime(t *testing.T) {
	body := zipHD(t, hd("K5LEB", "A", "HA", "11/10/2026", ""))
	mod := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "l_amat") {
			http.ServeContent(w, r, "l_amat.zip", mod, bytes.NewReader(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := testClient(t, srv)
	client.API = ""
	// 02:30 UTC on the 11th is still the evening of the 10th in New York.
	client.Now = func() time.Time { return time.Date(2026, 11, 11, 2, 30, 0, 0, time.UTC) }
	if ok, err := client.Active(t.Context(), "K5LEB"); err != nil || !ok {
		t.Fatalf("expiration day %v %v", ok, err)
	}
	client.Now = func() time.Time { return time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC) }
	if ok, err := client.Active(t.Context(), "K5LEB"); err != nil || ok {
		t.Fatalf("next morning %v %v", ok, err)
	}
}

func TestLookupFailureHidesTheBody(t *testing.T) {
	const secret = "SECRET NAME"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/"+secret, http.StatusFound)
	}))
	defer srv.Close()
	client := testClient(t, srv)
	client.Weekly = srv.URL + "/complete/l_amat.zip"
	_, err := client.Active(t.Context(), "K5LEB")
	if err == nil || err.Error() != "fcc license lookup failed" || strings.Contains(err.Error(), secret) {
		t.Fatalf("err %v", err)
	}
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "9999999999")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer huge.Close()
	client = testClient(t, huge)
	client.API = ""
	client.Weekly = huge.URL + "/complete/l_amat.zip"
	client.Daily = ""
	_, err = client.Active(t.Context(), "K5LEB")
	if err == nil || strings.Contains(err.Error(), "9999999999") {
		t.Fatalf("size err %v", err)
	}
}

func TestPlainFCCRedirectIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://data.fcc.gov/download/pub/uls/complete/l_amat.zip", http.StatusFound)
	}))
	defer srv.Close()
	client := testClient(t, srv)
	client.API = ""
	client.Weekly = srv.URL + "/complete/l_amat.zip"
	client.Daily = ""
	ok, err := client.Active(t.Context(), "K5LEB")
	if err == nil || ok || err.Error() != "fcc license lookup failed" {
		t.Fatalf("%v %v", ok, err)
	}
}

func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	client := New()
	client.API = srv.URL + "/api"
	client.Weekly = srv.URL + "/complete/l_amat.zip"
	client.Daily = srv.URL + "/daily/l_am_%s.zip"
	client.Now = func() time.Time { return time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC) }
	client.HTTP.Timeout = 5 * time.Second
	return client
}

func zipHD(t *testing.T, lines string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("HD.dat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, lines); err != nil {
		t.Fatal(err)
	}
	en, err := zw.Create("EN.dat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(en, "EN|SECRET NAME|0009999999\n"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hd(call, status, service, expired, cancelled string) string {
	return fmt.Sprintf("HD|1|||%s|%s|%s|01/01/2020|%s|%s|\n", call, status, service, expired, cancelled)
}
