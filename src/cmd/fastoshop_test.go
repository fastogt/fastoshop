package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func socketClient(path string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
}

func serveOnSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func TestListenUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.sock")
	serveOnSocket(t, path)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0660 {
		t.Fatalf("socket perms %o, want 660", fi.Mode().Perm())
	}

	resp, err := socketClient(path).Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

// Процесс, убитый SIGKILL, оставляет файл сокета — рестарт обязан его пережить.
func TestListenRemovesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	// Оставляем файл на месте — так выглядит сокет после SIGKILL.
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket file missing: %v", err)
	}

	serveOnSocket(t, path)
	resp, err := socketClient(path).Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestListenTCP(t *testing.T) {
	ln, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if ln.Addr().Network() != "tcp" {
		t.Fatalf("network %q, want tcp", ln.Addr().Network())
	}
}
