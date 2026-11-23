//go:build linux

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
	go func() {
		_ = serve(mux.Server(agentEnd), sock, "/mnt/wsl/e/distros", nil, nil, slog.New(slog.DiscardHandler))
	}()
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
	ensured := 0
	ensure := func(context.Context) error { ensured++; return nil }
	srv, err := serveDistro(runDir, "Ubuntu", sock, "/mnt/wsl/e/distros", []string{"HTTP_PROXY=http://p:3128"}, ensure, nil, slog.New(slog.DiscardHandler))
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
	if !strings.Contains(got, `"/mnt/wsl/e/distros/Ubuntu/home/me/proj:/src"`) || !strings.Contains(got, `"/mnt/c/x:/c"`) ||
		!strings.Contains(got, `"HTTP_PROXY=http://p:3128"`) {
		t.Errorf("engine got %s", got)
	}
	if ensured != 1 {
		t.Errorf("mount check ran %d times, want 1", ensured)
	}
	if _, err := serveDistro(runDir, "../etc", sock, "", nil, nil, nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("accepted a distro name with a path separator")
	}
}

func TestContainerProxyEnv(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		p := filepath.Join(dir, "proxy.env")
		_ = os.WriteFile(p, []byte(content), 0o600)
		return p
	}
	log := slog.New(slog.DiscardHandler)
	got := containerProxyEnv(write("export HTTP_PROXY=http://proxy:8080\nexport NO_PROXY=localhost,.corp\nexport PATH=/bin\n"), log)
	if strings.Join(got, " ") != "HTTP_PROXY=http://proxy:8080 NO_PROXY=localhost,.corp" {
		t.Errorf("env = %v", got)
	}
	if got := containerProxyEnv(write("export HTTPS_PROXY=http://alice:secret@proxy:8080\n"), log); got != nil {
		t.Errorf("passed a proxy with credentials: %v", got)
	}
	if got := containerProxyEnv(filepath.Join(dir, "missing"), log); got != nil {
		t.Errorf("missing file: %v", got)
	}
}

func TestIsMountpoint(t *testing.T) {
	info := filepath.Join(t.TempDir(), "mountinfo")
	_ = os.WriteFile(info, []byte(`22 1 8:32 / / rw,relatime - ext4 /dev/sdc rw
91 60 8:48 / /mnt/wsl/e/distros/Ubuntu rw,relatime shared:1 - ext4 /dev/sdd rw
92 60 8:48 / /mnt/wsl/e/distros/My\040Distro rw,relatime shared:1 - ext4 /dev/sdd rw
`), 0o644)
	for path, want := range map[string]bool{
		"/mnt/wsl/e/distros/Ubuntu":    true,
		"/mnt/wsl/e/distros/My Distro": true,
		"/mnt/wsl/e/distros/Debian":    false,
	} {
		if got, err := isMountpoint(info, path); err != nil || got != want {
			t.Errorf("isMountpoint(%q) = %v, %v; want %v", path, got, err, want)
		}
	}
}

func TestEnsureMountedAsksTheHost(t *testing.T) {
	hostEnd, agentEnd := net.Pipe()
	host, agent := mux.Client(hostEnd), mux.Server(agentEnd)
	defer host.Close()
	go func() {
		st, err := host.Accept()
		if err != nil {
			return
		}
		if string(st.Meta()) == controlMount+"Ubuntu" {
			_, _ = st.Write([]byte("ok\n"))
		}
		_ = st.CloseWrite()
	}()
	// The path is not a mount point here, so the host is asked.
	if err := ensureMounted(context.Background(), agent, "/nonexistent/distros/Ubuntu", "Ubuntu"); err != nil {
		t.Fatal(err)
	}
}

func TestNoticeSenderSendsEachNoticeOnce(t *testing.T) {
	hostEnd, agentEnd := net.Pipe()
	host, agent := mux.Client(hostEnd), mux.Server(agentEnd)
	defer host.Close()
	notify := noticeSender(agent)
	got := make(chan string, 4)
	go func() {
		for {
			st, err := host.Accept()
			if err != nil {
				return
			}
			got <- string(st.Meta())
		}
	}()
	notify("C: is slow")
	notify("C: is slow")
	notify("D: is slow")
	first, second := <-got, <-got
	if first != controlNotice+"C: is slow" || second != controlNotice+"D: is slow" {
		t.Fatalf("notices = %q, %q", first, second)
	}
	select {
	case extra := <-got:
		t.Fatalf("duplicate notice %q", extra)
	default:
	}
}
