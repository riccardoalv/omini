package webui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func port(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

func page(title string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>\n  " + title + "  </title></head></html>"))
	}
}

func newProber(ports ...Port) *Prober {
	p := &Prober{Ports: ports, Timeout: time.Second, TTL: time.Minute}
	p.init()
	return p
}

func TestFindsHTTPAndHTTPSInterfaces(t *testing.T) {
	plain := httptest.NewServer(page("Pi-hole &amp; DNS"))
	defer plain.Close()
	secure := httptest.NewTLSServer(page("OPNsense"))
	defer secure.Close()
	closed := httptest.NewServer(page("gone"))
	closedPort := port(t, closed)
	closed.Close()

	p := newProber(Port{port(t, secure), false}, Port{port(t, plain), true}, Port{closedPort, false})
	got, err := p.Find(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("found %+v, want 2 services", got)
	}
	// Order follows the configured ports; the scheme is detected even when the hint is wrong.
	if got[0].Title != "OPNsense" || got[0].URL != "https://"+secure.Listener.Addr().String()+"/" {
		t.Errorf("first service: %+v", got[0])
	}
	if got[1].Title != "Pi-hole & DNS" || got[1].URL != "http://"+plain.Listener.Addr().String()+"/" {
		t.Errorf("second service: %+v", got[1])
	}
}

func TestNonHTTPPortIsIgnored(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // accepts and closes immediately, like SSH or a database would not speak HTTP
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	p := newProber(Port{ln.Addr().(*net.TCPAddr).Port, false})
	got, _ := p.Find(context.Background(), "127.0.0.1")
	if len(got) != 0 {
		t.Fatalf("non-HTTP port reported as web interface: %+v", got)
	}
}

func TestSameInterfaceOnTwoPortsIsReportedOnce(t *testing.T) {
	a := httptest.NewServer(page("Router admin"))
	defer a.Close()
	b := httptest.NewServer(page("Router admin"))
	defer b.Close()
	p := newProber(Port{port(t, a), false}, Port{port(t, b), false})
	got, _ := p.Find(context.Background(), "127.0.0.1")
	if len(got) != 1 || got[0].Port != port(t, a) {
		t.Fatalf("got %+v, want only the first port", got)
	}
}

func TestResultsAreCached(t *testing.T) {
	srv := httptest.NewServer(page("NAS"))
	p := newProber(Port{port(t, srv), false})
	clock := time.Now()
	p.now = func() time.Time { return clock }

	first, _ := p.Find(context.Background(), "127.0.0.1")
	srv.Close()
	cached, _ := p.Find(context.Background(), "127.0.0.1")
	if len(first) != 1 || len(cached) != 1 {
		t.Fatalf("cache not used: first=%v cached=%v", first, cached)
	}
	clock = clock.Add(2 * time.Minute)
	fresh, _ := p.Find(context.Background(), "127.0.0.1")
	if len(fresh) != 0 {
		t.Fatalf("expired cache should probe again, got %v", fresh)
	}
}

func TestInvalidIP(t *testing.T) {
	if _, err := New().Find(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestErrorPagesHaveNoTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<html><title>400 The plain HTTP request was sent to HTTPS port</title></html>"))
	}))
	defer srv.Close()
	got, _ := newProber(Port{port(t, srv), false}).Find(context.Background(), "127.0.0.1")
	if len(got) != 1 || got[0].Title != "" {
		t.Fatalf("got %+v, want the service without an error title", got)
	}
}
