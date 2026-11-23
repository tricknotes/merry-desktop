package wsl

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Versions of what goes into the engine distro. Bumping them is how the engine
// version is pinned (F-13 will make this configurable by administrators).
const (
	alpineVersion  = "3.24.4"
	alpineSHA256   = "cdaa38cb8de3537967ee5b6c9b6cd00cc962da2b018e6eea48696bb274923012"
	dockerVersion  = "29.10.0"
	composeVersion = "v5.8.0"
)

//go:embed assets/*.sh
var assets embed.FS

const setupDir = "/opt/engine-setup"

var _ vm.Provisioner = (*Backend)(nil)

func defaultInstallRoot(distro string) string {
	dir, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		return ""
	}
	return filepath.Join(dir, distro)
}

func alpineURL() string {
	minor := strings.Join(strings.SplitN(alpineVersion, ".", 3)[:2], ".")
	return fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v%s/releases/x86_64/alpine-minirootfs-%s-x86_64.tar.gz", minor, alpineVersion)
}

// CheckHost checks that WSL is installed and usable by this user. Enabling WSL
// needs an administrator, which the app does not automate (REQUIREMENTS.md 8).
func (b *Backend) CheckHost(ctx context.Context) error {
	res, err := b.Runner.Run(ctx, "--status")
	if err != nil {
		return fmt.Errorf("%w: WSL が見つかりません。管理者に WSL2 の有効化を依頼してください(コマンド: wsl --install --no-distribution)", vm.ErrHostNotReady)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%w: WSL2 が使える状態になっていません。管理者に WSL2 の有効化を依頼してください(コマンド: wsl --install --no-distribution)。詳細: %s",
			vm.ErrHostNotReady, strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return nil
}

func (b *Backend) Provisioned(ctx context.Context) (bool, error) {
	_, err := b.lookup(ctx)
	if errors.Is(err, vm.ErrNotProvisioned) {
		return false, nil
	}
	return err == nil, err
}

// Provision creates the engine distro (see docs/phase0-wsl2-backend.md):
// download the Alpine rootfs, import it, copy the setup scripts and CA
// certificates in, run provision.sh and stop the distro, so that the next start
// runs its [boot] command. On failure the half-made distro is removed.
func (b *Backend) Provision(ctx context.Context, network vm.Network, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	if ok, err := b.Provisioned(ctx); err != nil {
		return err
	} else if ok {
		progress("VM はすでにあります")
		return nil
	}

	progress("Alpine Linux " + alpineVersion + " をダウンロードしています")
	rootfs, err := b.downloadRootfs(ctx, network)
	if err != nil {
		return scrubErr(err, network.ProxyURL)
	}

	progress("VM を作成しています")
	disk := filepath.Join(b.InstallRoot, "disk")
	if err := os.MkdirAll(disk, 0o755); err != nil {
		return err
	}
	if err := b.mustRun(ctx, RunOptions{}, "--import", b.Distro, disk, rootfs, "--version", "2"); err != nil {
		return err
	}
	if err := b.provisionImported(ctx, network, progress); err != nil {
		// Leave nothing half-made behind, so that setup can simply run again.
		_, _ = b.Runner.Run(context.WithoutCancel(ctx), "--unregister", b.Distro)
		return scrubErr(err, network.ProxyURL)
	}
	return nil
}

func (b *Backend) provisionImported(ctx context.Context, network vm.Network, progress func(string)) error {
	bundle, certCount, err := setupBundle(network.CACertsPEM)
	if err != nil {
		return err
	}
	if err := b.mustRun(ctx, RunOptions{Stdin: bundle},
		b.inDistro("sh", "-c", "mkdir -p "+setupDir+" && tar -x -C "+setupDir)...); err != nil {
		return fmt.Errorf("copy setup files: %w", err)
	}
	if certCount > 0 {
		progress(fmt.Sprintf("証明書を %d 件取り込みます", certCount))
	}

	progress("Docker Engine " + dockerVersion + " を入れています(数分かかることがあります)")
	env := map[string]string{
		"ENGINE_NAME":     b.Distro,
		"DOCKER_VERSION":  dockerVersion,
		"COMPOSE_VERSION": composeVersion,
		"ENGINE_PROXY":    network.ProxyURL,
		"ENGINE_NO_PROXY": network.NoProxy,
		"ENGINE_CA_DIR":   setupDir + "/ca",
	}
	if err := b.mustRun(ctx, RunOptions{Env: env}, b.inDistro("sh", setupDir+"/provision.sh")...); err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	// Stop it, so that the next start runs the [boot] command from /etc/wsl.conf.
	return b.mustRun(ctx, RunOptions{}, "--terminate", b.Distro)
}

// mustRun runs wsl.exe and turns a non-zero exit into an error with the last
// lines of output.
func (b *Backend) mustRun(ctx context.Context, opts RunOptions, args ...string) error {
	res, err := b.Runner.RunWith(ctx, opts, args...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("wsl %s: exit %d: %s", args[0], res.ExitCode, lastLines(res.Stderr+res.Stdout, 15))
	}
	return nil
}

func (b *Backend) downloadRootfs(ctx context.Context, network vm.Network) (string, error) {
	dir := filepath.Join(b.InstallRoot, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, path.Base(b.RootfsURL))
	if ok, _ := fileHasSHA256(target, b.RootfsSHA256); ok {
		return target, nil
	}

	client := b.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if network.ProxyURL != "" {
			u, err := url.Parse(network.ProxyURL)
			if err != nil {
				return "", errors.New("プロキシの URL が正しくありません")
			}
			transport.Proxy = http.ProxyURL(u)
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.RootfsURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", b.RootfsURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", b.RootfsURL, resp.Status)
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("download %s: %w", b.RootfsURL, err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != b.RootfsSHA256 {
		return "", fmt.Errorf("download %s: SHA-256 mismatch (got %s, want %s)", b.RootfsURL, got, b.RootfsSHA256)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", err
	}
	return target, nil
}

func fileHasSHA256(name, want string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == want, nil
}

// setupBundle builds a tar with the setup scripts and one file per CA
// certificate (update-ca-certificates expects one certificate per file).
func setupBundle(caPEM []byte) ([]byte, int, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, mode int64, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	scripts, err := fs.Glob(assets, "assets/*.sh")
	if err != nil {
		return nil, 0, err
	}
	for _, s := range scripts {
		data, err := assets.ReadFile(s)
		if err != nil {
			return nil, 0, err
		}
		if err := add(path.Base(s), 0o755, data); err != nil {
			return nil, 0, err
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: "ca/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Unix(0, 0)}); err != nil {
		return nil, 0, err
	}
	count := 0
	for rest := caPEM; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		count++
		if err := add(fmt.Sprintf("ca/host-%03d.crt", count), 0o644, pem.EncodeToMemory(block)); err != nil {
			return nil, 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func scrubErr(err error, proxy string) error {
	if err == nil || proxy == "" {
		return err
	}
	return errors.New(scrub(err.Error(), proxy))
}
