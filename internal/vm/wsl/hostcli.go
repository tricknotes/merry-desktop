package wsl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// The docker CLI for the Windows host (F-04). Both come from the open source
// releases (download.docker.com static binaries, docker/compose on GitHub),
// not from Docker Desktop. The zip has no published checksum, so it is pinned.
const (
	dockerCLIZipURL    = "https://download.docker.com/win/static/stable/x86_64/docker-" + dockerVersion + ".zip"
	dockerCLIZipSHA256 = "bb894f67d85155e635ce71980fb2cf587e71dad8c41cc145c42a0de5160b6561"
	composeExeURL      = "https://github.com/docker/compose/releases/download/" + composeVersion + "/docker-compose-windows-x86_64.exe"
	composeExeSHA256   = "0c2d9245a5f61e8e3cc85009b8b4888fa82fded08239d28bd6da5ca818eda2b2"
)

// defaultPipe is where the Windows docker CLI connects without a context.
const defaultPipe = "npipe:////./pipe/docker_engine"

var _ vm.HostIntegrator = (*Backend)(nil)

// hostCLI describes where the host CLI goes. Fields are set by New and
// replaced in tests.
type hostCLI struct {
	BinDir                    string // %LOCALAPPDATA%\<distro>\bin, added to the user PATH
	PluginsDir                string // %USERPROFILE%\.docker\cli-plugins
	DockerURL, DockerSHA256   string
	ComposeURL, ComposeSHA256 string
}

func defaultHostCLI(installRoot string) hostCLI {
	home, _ := os.UserHomeDir()
	return hostCLI{
		BinDir:        filepath.Join(installRoot, "bin"),
		PluginsDir:    filepath.Join(home, ".docker", "cli-plugins"),
		DockerURL:     dockerCLIZipURL,
		DockerSHA256:  dockerCLIZipSHA256,
		ComposeURL:    composeExeURL,
		ComposeSHA256: composeExeSHA256,
	}
}

// IntegrateHost makes `docker` and `docker compose` work in a new terminal:
// installs the CLI, adds it to the user PATH, and creates a docker context
// when the engine could not use the default pipe name.
func (b *Backend) IntegrateHost(ctx context.Context, network vm.Network, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	c := b.HostCLI
	docker := filepath.Join(c.BinDir, "docker.exe")
	if !b.cliCurrent(docker) {
		progress("docker CLI " + dockerVersion + " をダウンロードしています")
		zipData, err := b.download(ctx, network, c.DockerURL, c.DockerSHA256)
		if err != nil {
			return scrubErr(err, network.ProxyURL)
		}
		exe, err := extractFromZip(zipData, "docker/docker.exe")
		if err != nil {
			return err
		}
		if err := writeFileAtomic(docker, exe); err != nil {
			return err
		}
		if err := os.WriteFile(docker+".sha256", []byte(c.DockerSHA256), 0o644); err != nil {
			return err
		}
	}

	compose := filepath.Join(c.PluginsDir, "docker-compose.exe")
	if _, err := os.Stat(compose); errors.Is(err, os.ErrNotExist) {
		// An existing plugin is the user's own; it is left alone.
		progress("Docker Compose " + composeVersion + " をダウンロードしています")
		exe, err := b.download(ctx, network, c.ComposeURL, c.ComposeSHA256)
		if err != nil {
			return scrubErr(err, network.ProxyURL)
		}
		if err := writeFileAtomic(compose, exe); err != nil {
			return err
		}
	}

	progress("PATH に追加しています")
	if err := addToUserPath(c.BinDir); err != nil {
		return fmt.Errorf("add %s to PATH: %w", c.BinDir, err)
	}

	if ep := b.HostEndpoint(); ep != "" && ep != defaultPipe {
		progress("docker context を作成しています")
		if err := b.ensureHostContext(ctx, docker, ep); err != nil {
			return err
		}
	}
	return nil
}

// cliCurrent reports whether the installed CLI is the pinned version.
func (b *Backend) cliCurrent(docker string) bool {
	if _, err := os.Stat(docker); err != nil {
		return false
	}
	got, err := os.ReadFile(docker + ".sha256")
	return err == nil && strings.TrimSpace(string(got)) == b.HostCLI.DockerSHA256
}

// ensureHostContext creates a context for a non-default pipe and switches to
// it only while the user is still on the default context.
func (b *Backend) ensureHostContext(ctx context.Context, docker, endpoint string) error {
	run := func(args ...string) (string, error) {
		out, err := runHostCommand(ctx, docker, args...)
		return strings.TrimSpace(out), err
	}
	if _, err := run("context", "inspect", b.Distro); err != nil {
		if _, err := run("context", "create", b.Distro, "--description", "container engine VM", "--docker", "host="+endpoint); err != nil {
			return fmt.Errorf("docker context create: %w", err)
		}
	}
	if current, err := run("context", "show"); err == nil && current == "default" {
		if _, err := run("context", "use", b.Distro); err != nil {
			return fmt.Errorf("docker context use: %w", err)
		}
	}
	return nil
}

// download fetches url through the detected proxy and checks its SHA-256.
func (b *Backend) download(ctx context.Context, network vm.Network, rawURL, wantSHA256 string) ([]byte, error) {
	client := b.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if network.ProxyURL != "" {
			u, err := url.Parse(network.ProxyURL)
			if err != nil {
				return nil, errors.New("プロキシの URL が正しくありません")
			}
			transport.Proxy = http.ProxyURL(u)
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", rawURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantSHA256 {
		return nil, fmt.Errorf("download %s: SHA-256 mismatch (got %s, want %s)", rawURL, got, wantSHA256)
	}
	return data, nil
}

func extractFromZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 256<<20))
	}
	return nil, fmt.Errorf("%s not found in the archive", name)
}

// withPathEntry returns the PATH value with dir appended, unless it is already
// there (compared case-insensitively, ignoring a trailing backslash, and after
// expanding %VARIABLES%). Appending keeps another docker.exe the user already
// has ahead of ours.
func withPathEntry(path, dir string, expand func(string) string) (string, bool) {
	norm := func(s string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(expand(s)), `\/`))
	}
	want := norm(dir)
	for _, e := range strings.Split(path, ";") {
		if e != "" && norm(e) == want {
			return path, false
		}
	}
	trimmed := strings.TrimRight(path, ";")
	if trimmed == "" {
		return dir, true
	}
	return trimmed + ";" + dir, true
}
