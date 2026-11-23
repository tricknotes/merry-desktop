package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
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
