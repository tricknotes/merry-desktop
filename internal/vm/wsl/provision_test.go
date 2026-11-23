package wsl

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

const testCert = `-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUQ6m0vH0XzkWlnXzDiKZb3g0Q4lIwCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
`

// provisionFixture serves a fake rootfs and records what Provision asks wsl.exe to do.
type provisionFixture struct {
	runner    *fakeRunner
	backend   *Backend
	tarFiles  map[string]string
	provEnv   map[string]string
	provArgs  string
	provFails bool
	imported  bool
}

func newProvisionFixture(t *testing.T) *provisionFixture {
	t.Helper()
	rootfs := []byte("fake rootfs")
	sum := sha256.Sum256(rootfs)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rootfs)
	}))
	t.Cleanup(srv.Close)

	fx := &provisionFixture{tarFiles: map[string]string{}}
	fx.runner = &fakeRunner{} // not installed yet
	fx.runner.handle = func(opts RunOptions, args []string) (Result, bool) {
		joined := strings.Join(args, " ")
		switch {
		case args[0] == "--import":
			fx.imported = true
			fx.runner.state = "Stopped"
			return Result{}, true
		case args[0] == "--unregister":
			fx.runner.state = ""
			return Result{}, true
		case strings.HasSuffix(joined, "tar -x -C /opt/engine-setup"):
			tr := tar.NewReader(bytes.NewReader(opts.Stdin))
			for {
				h, err := tr.Next()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(tr)
				fx.tarFiles[h.Name] = string(data)
			}
			return Result{}, true
		case strings.HasSuffix(joined, "sh /opt/engine-setup/provision.sh"):
			fx.provEnv, fx.provArgs = opts.Env, joined
			if fx.provFails {
				return Result{ExitCode: 7, Stderr: "curl: (7) Failed to connect to http://bob:hunter2@proxy.example:8080\n"}, true
			}
			return Result{Stdout: "[provision] done\n"}, true
		}
		return Result{}, false
	}
	b := newTestBackend(t, fx.runner)
	b.InstallRoot = t.TempDir()
	b.RootfsURL = srv.URL + "/alpine-minirootfs-test.tar.gz"
	b.RootfsSHA256 = hex.EncodeToString(sum[:])
	b.HTTPClient = srv.Client()
	fx.backend = b
	return fx
}

func TestProvision(t *testing.T) {
	fx := newProvisionFixture(t)
	network := vm.Network{ProxyURL: "http://bob:hunter2@proxy.example:8080", NoProxy: "localhost", CACertsPEM: []byte(testCert + testCert)}
	var progress []string
	if err := fx.backend.Provision(context.Background(), network, func(s string) { progress = append(progress, s) }); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !fx.imported {
		t.Fatal("distro was not imported")
	}
	for _, name := range []string{"provision.sh", "apply-network.sh", "engine-start.sh", "ca/host-001.crt", "ca/host-002.crt"} {
		if _, ok := fx.tarFiles[name]; !ok {
			t.Errorf("setup bundle lacks %s (has %v)", name, keys(fx.tarFiles))
		}
	}
	if fx.provEnv["ENGINE_PROXY"] != network.ProxyURL || fx.provEnv["DOCKER_VERSION"] != dockerVersion || fx.provEnv["ENGINE_NAME"] != "test-engine" {
		t.Errorf("provision env = %v", fx.provEnv)
	}
	if strings.Contains(fx.provArgs, "hunter2") {
		t.Error("proxy password on the command line")
	}
	if last := fx.runner.calls[len(fx.runner.calls)-1]; last != "--terminate test-engine" {
		t.Errorf("last call = %q, want terminate", last)
	}
	if len(progress) == 0 {
		t.Error("no progress reported")
	}

	// A second run finds the distro and does nothing.
	fx.imported = false
	if err := fx.backend.Provision(context.Background(), network, nil); err != nil || fx.imported {
		t.Fatalf("second Provision: err=%v imported=%v", err, fx.imported)
	}
}

func TestProvisionFailureCleansUpAndHidesPassword(t *testing.T) {
	fx := newProvisionFixture(t)
	fx.provFails = true
	err := fx.backend.Provision(context.Background(), vm.Network{ProxyURL: "http://bob:hunter2@proxy.example:8080"}, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks the proxy password: %v", err)
	}
	if ok, _ := fx.backend.Provisioned(context.Background()); ok {
		t.Error("half-made distro was not removed")
	}
}

func TestProvisionRejectsBadDownload(t *testing.T) {
	fx := newProvisionFixture(t)
	fx.backend.RootfsSHA256 = strings.Repeat("0", 64)
	err := fx.backend.Provision(context.Background(), vm.Network{}, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("err = %v, want SHA-256 mismatch", err)
	}
	if fx.imported {
		t.Error("imported a rootfs with a wrong checksum")
	}
	matches, _ := filepath.Glob(filepath.Join(fx.backend.InstallRoot, "cache", "*"))
	if len(matches) != 0 {
		t.Errorf("left files in the cache: %v", matches)
	}
}

func TestCheckHost(t *testing.T) {
	ok := newTestBackend(t, &fakeRunner{handle: func(_ RunOptions, args []string) (Result, bool) {
		return Result{Stdout: "Default Version: 2"}, args[0] == "--status"
	}})
	if err := ok.CheckHost(context.Background()); err != nil {
		t.Fatalf("CheckHost: %v", err)
	}
	notReady := newTestBackend(t, &fakeRunner{handle: func(_ RunOptions, args []string) (Result, bool) {
		return Result{ExitCode: 1, Stdout: "WSL is not installed"}, args[0] == "--status"
	}})
	if err := notReady.CheckHost(context.Background()); !errors.Is(err, vm.ErrHostNotReady) {
		t.Fatalf("CheckHost = %v, want ErrHostNotReady", err)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDestroyResetsTheAppliedDiskLimit(t *testing.T) {
	fx := newProvisionFixture(t)
	if err := fx.backend.Provision(context.Background(), vm.Network{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := fx.backend.saveState(state{DiskGiB: 64, AppliedDiskGiB: 64}); err != nil {
		t.Fatal(err)
	}
	if err := fx.backend.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := fx.backend.Provisioned(context.Background()); ok {
		t.Error("distro still registered")
	}
	s, _ := fx.backend.loadState()
	if s.DiskGiB != 64 || s.AppliedDiskGiB != 0 {
		t.Errorf("state = %+v, want the limit kept and marked as not applied", s)
	}
	// Destroying a VM that does not exist is fine.
	if err := fx.backend.Destroy(context.Background()); err != nil {
		t.Errorf("second Destroy: %v", err)
	}
}
