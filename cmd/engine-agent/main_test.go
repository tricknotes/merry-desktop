package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/mux"
)

// TestServeTranslatesPerClientKind runs the agent's serve loop against a fake
// engine and checks that only Windows clients get their paths rewritten.
func TestServeTranslatesPerClientKind(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 4)
	engine := &httptest.Server{Listener: l, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- string(b)
		w.WriteHeader(http.StatusCreated)
	})}}
	engine.Start()
	defer engine.Close()

	hostEnd, agentEnd := net.Pipe()
	go func() { _ = serve(agentEnd, sock, "/mnt/wsl/e/distros", slog.New(slog.DiscardHandler)) }()
	session := mux.Client(hostEnd)
	defer session.Close()

	post := func(meta string) string {
		hc := &http.Client{Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) { return session.Open([]byte(meta)) },
		}}
		resp, err := hc.Post("http://docker/v1.57/containers/create", "application/json",
			strings.NewReader(`{"HostConfig":{"Binds":["C:\\proj:/src","\\\\wsl$\\Ubuntu\\home\\me:/home"]}}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return <-received
	}
	if got := post(metaWindows); !strings.Contains(got, `"/mnt/c/proj:/src"`) || !strings.Contains(got, `"/mnt/wsl/e/distros/Ubuntu/home/me:/home"`) {
		t.Errorf("windows client: engine got %s", got)
	}
	if got := post("other"); !strings.Contains(got, `C:\\proj:/src`) {
		t.Errorf("other client was rewritten: %s", got)
	}
}

func TestServeDistroSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	engine := &httptest.Server{Listener: l, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- string(b)
	})}}
	engine.Start()
	defer engine.Close()

	runDir := filepath.Join(dir, "run")
	srv, err := serveDistro(runDir, "Ubuntu", sock, "/mnt/wsl/e/distros", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	distroSock := filepath.Join(runDir, "Ubuntu", "docker.sock")
	if fi, err := os.Stat(distroSock); err != nil || fi.Mode().Perm() != 0o666 {
		t.Fatalf("socket %v, %v", fi, err)
	}
	hc := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return net.Dial("unix", distroSock)
	}}}
	resp, err := hc.Post("http://docker/containers/create", "application/json",
		strings.NewReader(`{"HostConfig":{"Binds":["/home/me/proj:/src","/mnt/c/x:/c"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got := <-received
	if !strings.Contains(got, `"/mnt/wsl/e/distros/Ubuntu/home/me/proj:/src"`) || !strings.Contains(got, `"/mnt/c/x:/c"`) {
		t.Errorf("engine got %s", got)
	}
	if _, err := serveDistro(runDir, "../etc", sock, "", slog.New(slog.DiscardHandler)); err == nil {
		t.Error("accepted a distro name with a path separator")
	}
}
