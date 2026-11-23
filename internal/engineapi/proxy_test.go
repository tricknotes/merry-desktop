package engineapi

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngine listens on a unix socket like dockerd.
func fakeEngine(t *testing.T, h http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return sock
}

func TestProxyRewritesContainerCreate(t *testing.T) {
	var got string
	sock := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = r.Method + " " + r.URL.Path + " " + string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	proxy := httptest.NewServer(NewProxy(sock, WindowsPaths(""), nil))
	defer proxy.Close()

	resp, err := http.Post(proxy.URL+"/v1.57/containers/create?name=web", "application/json",
		strings.NewReader(`{"HostConfig":{"Binds":["C:\\src:/src"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || !strings.Contains(got, `"/mnt/c/src:/src"`) || !strings.HasPrefix(got, "POST /v1.57/containers/create") {
		t.Fatalf("status %d, engine got %s", resp.StatusCode, got)
	}

	// Other requests pass through untouched.
	resp, err = http.Post(proxy.URL+"/v1.57/build", "application/x-tar", strings.NewReader(`C:\src:/src`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.HasSuffix(got, `C:\src:/src`) {
		t.Fatalf("build body was changed: %s", got)
	}
}

func TestProxyPassesUpgrades(t *testing.T) {
	// dockerd answers exec/attach with 101 and then speaks raw bytes.
	sock := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"))
		line, _ := buf.ReadString('\n')
		_, _ = conn.Write([]byte("echo:" + line))
	}))
	proxy := httptest.NewServer(NewProxy(sock, nil, nil))
	defer proxy.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("POST /v1.57/exec/abc/start HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n"))
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("response %v, %v", resp, err)
	}
	_, _ = conn.Write([]byte("hello\n"))
	line, _ := r.ReadString('\n')
	if line != "echo:hello\n" {
		t.Fatalf("after upgrade got %q", line)
	}
}

func TestProxyRunsBeforeCreate(t *testing.T) {
	sock := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	calls := 0
	proxy := httptest.NewServer(NewProxyWith(sock, Options{BeforeCreate: func(context.Context) error {
		calls++
		return nil
	}}, nil))
	defer proxy.Close()
	for _, path := range []string{"/v1.57/containers/create", "/v1.57/containers/json", "/containers/create"} {
		resp, err := http.Post(proxy.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if calls != 2 {
		t.Fatalf("BeforeCreate ran %d times, want 2 (only for container create)", calls)
	}
}
