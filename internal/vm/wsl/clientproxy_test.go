package wsl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestConfigureClientProxy(t *testing.T) {
	distroConfig := `{"auths":{}}`
	var written []byte
	f := &fakeRunner{state: "Stopped"}
	f.handle = func(opts RunOptions, args []string) (Result, bool) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "--distribution Ubuntu --exec sh -c "+readDockerConfig):
			return Result{Stdout: distroConfig}, true
		case strings.Contains(joined, "--distribution Ubuntu --exec sh -c "+writeDockerConfig):
			written = opts.Stdin
			return Result{}, true
		}
		return Result{}, false
	}
	b := newTestBackend(t, f)
	b.HostDockerConfig = filepath.Join(t.TempDir(), ".docker", "config.json")
	_ = os.MkdirAll(filepath.Dir(b.HostDockerConfig), 0o700)
	_ = os.WriteFile(b.HostDockerConfig, []byte(`{"proxies":{"default":{"httpProxy":"http://mine:1"}}}`), 0o600)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	p := vm.ClientProxy{HTTPProxy: "http://proxy:3128", HTTPSProxy: "http://proxy:3128", NoProxy: "localhost"}
	lines, err := b.ConfigureClientProxy(context.Background(), p, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "Windows: 利用者が設定したプロキシがあるため、変更しません|Ubuntu: 書き込みました" {
		t.Errorf("lines = %q", lines)
	}
	if !strings.Contains(string(written), `"httpsProxy": "http://proxy:3128"`) || !strings.Contains(string(written), `"auths"`) {
		t.Errorf("written to Ubuntu: %s", written)
	}
	if host, _ := os.ReadFile(b.HostDockerConfig); !strings.Contains(string(host), "mine") {
		t.Errorf("the user's own host proxy was changed: %s", host)
	}

	// Disabling removes what was written.
	distroConfig = string(written)
	lines, err = b.ConfigureClientProxy(context.Background(), p, false)
	if err != nil || lines[1] != "Ubuntu: 取り除きました" || strings.Contains(string(written), "proxy:3128") {
		t.Errorf("disable: lines=%q err=%v written=%s", lines, err, written)
	}
}

func TestConfigureClientProxyWritesAndBacksUpHostFile(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{state: "Stopped"})
	b.HostDockerConfig = filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(b.HostDockerConfig, []byte(`{"credsStore":"wincred"}`), 0o600)
	p := vm.ClientProxy{HTTPProxy: "http://proxy:3128"}
	lines, err := b.ConfigureClientProxy(context.Background(), p, true)
	if err != nil || lines[0] != "Windows: 書き込みました" {
		t.Fatalf("lines=%q err=%v", lines, err)
	}
	if backup, _ := os.ReadFile(b.HostDockerConfig + ".bak-20261123-100000"); string(backup) != `{"credsStore":"wincred"}` {
		t.Errorf("backup = %q", backup)
	}
	got, _ := os.ReadFile(b.HostDockerConfig)
	if !strings.Contains(string(got), "wincred") || !strings.Contains(string(got), "proxy:3128") {
		t.Errorf("config = %s", got)
	}
}
