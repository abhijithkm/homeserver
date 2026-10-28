package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/store"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startRun(t *testing.T, mutate func(*store.Settings)) (*Server, int, context.CancelFunc, chan error) {
	t.Helper()
	d := paths.Resolve(t.TempDir())
	st, _, err := store.Open(d.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set, _ := st.GetSettings()
	set.Port, set.BindAddr = freePort(t), "127.0.0.1"
	if mutate != nil {
		mutate(&set)
	}
	if err := st.SaveSettings(set); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Store: st, Dirs: d, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return s, set.Port, cancel, done
}

func waitHealthy(t *testing.T, scheme string, port int) {
	t.Helper()
	c := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	for i := 0; i < 50; i++ {
		resp, err := c.Get(fmt.Sprintf("%s://127.0.0.1:%d/healthz", scheme, port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("server never became healthy")
}

func TestRunGracefulShutdownReleasesPort(t *testing.T) {
	s, port, cancel, done := startRun(t, nil)
	waitHealthy(t, "http", port)
	if _, err := os.Stat(s.cfg.Dirs.Runtime); err != nil {
		t.Fatal("runtime.json not written")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown hung")
	}
	if _, err := os.Stat(s.cfg.Dirs.Runtime); err == nil {
		t.Fatal("runtime.json left behind")
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port not released: %v", err)
	}
	l.Close()
}

func TestRunRestartRequest(t *testing.T) {
	s, port, cancel, done := startRun(t, nil)
	defer cancel()
	waitHealthy(t, "http", port)
	s.RequestRestart()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRestart) {
			t.Fatalf("got %v, want ErrRestart", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("restart hung")
	}
}

func TestRunPortConflictGivesClearError(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, _, cancel, done := startRun(t, func(s *store.Settings) { s.Port = busy.Addr().(*net.TCPAddr).Port })
	defer cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
		t.Logf("port conflict error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not fail on a busy port")
	}
}

func TestRunSelfSignedTLS(t *testing.T) {
	s, port, cancel, done := startRun(t, func(s *store.Settings) { s.TLSMode = "selfsigned" })
	waitHealthy(t, "https", port)
	resp, err := (&http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}).Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS header missing over TLS")
	}
	if resp.TLS == nil || resp.TLS.Version < tls.VersionTLS12 {
		t.Fatal("TLS 1.2+ not negotiated")
	}
	if _, err := os.Stat(s.cfg.Dirs.TLS); err != nil {
		t.Fatal("certificate folder missing")
	}
	cancel()
	<-done
}
