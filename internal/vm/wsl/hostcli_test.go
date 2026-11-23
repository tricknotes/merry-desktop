package wsl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestWithPathEntry(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\me\AppData\Local`) }
	dir := `C:\Users\me\AppData\Local\engine\bin`
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"", dir, true},
		{`C:\tools`, `C:\tools;` + dir, true},
		{`C:\tools;`, `C:\tools;` + dir, true},
		{`C:\tools;c:\users\ME\appdata\local\engine\bin\`, `C:\tools;c:\users\ME\appdata\local\engine\bin\`, false},
		{`%LOCALAPPDATA%\engine\bin;C:\tools`, `%LOCALAPPDATA%\engine\bin;C:\tools`, false},
	}
	for _, c := range cases {
		got, changed := withPathEntry(c.in, dir, expand)
		if got != c.want || changed != c.changed {
			t.Errorf("withPathEntry(%q) = %q, %v; want %q, %v", c.in, got, changed, c.want, c.changed)
		}
	}
}

func zipWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(data))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestIntegrateHostInstallsCLIAndKeepsUsersPlugin(t *testing.T) {
	zipData := zipWith(t, map[string]string{"docker/docker.exe": "cli", "docker/dockerd.exe": "daemon"})
	compose := []byte("compose")
	downloads := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads[r.URL.Path]++
		if r.URL.Path == "/docker.zip" {
			_, _ = w.Write(zipData)
			return
		}
		_, _ = w.Write(compose)
	}))
	defer srv.Close()

	b := newTestBackend(t, &fakeRunner{})
	dir := t.TempDir()
	b.HTTPClient = srv.Client()
	b.HostCLI = hostCLI{
		BinDir: filepath.Join(dir, "bin"), PluginsDir: filepath.Join(dir, "plugins"),
		DockerURL: srv.URL + "/docker.zip", DockerSHA256: sha(zipData),
		ComposeURL: srv.URL + "/compose.exe", ComposeSHA256: sha(compose),
	}
	if err := b.IntegrateHost(context.Background(), vm.Network{}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bin", "docker.exe")); string(got) != "cli" {
		t.Errorf("docker.exe = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "dockerd.exe")); err == nil {
		t.Error("installed dockerd.exe; only the CLI is needed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plugins", "docker-compose.exe")); string(got) != "compose" {
		t.Errorf("compose plugin = %q", got)
	}

	// Second run: the CLI is current and the plugin exists, so nothing is downloaded.
	if err := b.IntegrateHost(context.Background(), vm.Network{}, nil); err != nil {
		t.Fatal(err)
	}
	if downloads["/docker.zip"] != 1 || downloads["/compose.exe"] != 1 {
		t.Errorf("downloads = %v, want one each", downloads)
	}

	// A plugin the user installed is not replaced.
	_ = os.WriteFile(filepath.Join(dir, "plugins", "docker-compose.exe"), []byte("users own"), 0o755)
	_ = os.Remove(filepath.Join(dir, "bin", "docker.exe"))
	if err := b.IntegrateHost(context.Background(), vm.Network{}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plugins", "docker-compose.exe")); string(got) != "users own" {
		t.Errorf("replaced the user's plugin: %q", got)
	}
}

func TestIntegrateHostRejectsTamperedDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer srv.Close()
	b := newTestBackend(t, &fakeRunner{})
	dir := t.TempDir()
	b.HTTPClient = srv.Client()
	b.HostCLI = hostCLI{BinDir: filepath.Join(dir, "bin"), PluginsDir: dir, DockerURL: srv.URL, DockerSHA256: strings.Repeat("0", 64)}
	err := b.IntegrateHost(context.Background(), vm.Network{}, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "docker.exe")); err == nil {
		t.Error("installed a file with a wrong checksum")
	}
}
