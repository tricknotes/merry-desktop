package wsl

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// agentGzip is cmd/engine-agent built for linux/amd64 and gzip-compressed.
// `make agent` builds it into assets/ before the app (it is not checked in).
//
//go:embed assets/engine-agent.gz
var agentGzip []byte

const agentPath = "/usr/local/bin/engine-agent"

// agentSHA256 is the checksum of the uncompressed agent, which is what
// sha256sum reports inside the distro.
var agentSHA256 = func() string {
	zr, err := gzip.NewReader(bytes.NewReader(agentGzip))
	if err != nil {
		return "invalid"
	}
	h := sha256.New()
	if _, err := io.Copy(h, zr); err != nil {
		return "invalid"
	}
	return hex.EncodeToString(h.Sum(nil))
}()

// agentCommand is the long-running session: it serves the engine API to the
// host and keeps the distro running (WSL stops distros without a session).
func (b *Backend) agentCommand() []string {
	return b.inDistro(agentPath, "stdio", "--engine-socket", "/var/run/docker.sock", "--distro-root", b.distroRoot())
}

// distroRoot is where other WSL distros' roots will be visible in the engine
// distro, for \\wsl$\<distro>\... paths from the Windows CLI.
func (b *Backend) distroRoot() string {
	return "/mnt/wsl/" + b.Distro + "/distros"
}

// ensureAgent installs the embedded agent into the distro unless the same
// build is already there. This also upgrades the agent after an app update.
func (b *Backend) ensureAgent(ctx context.Context) error {
	res, err := b.Runner.Run(ctx, b.inDistro("sha256sum", agentPath)...)
	if err != nil {
		return err
	}
	if res.ExitCode == 0 && strings.HasPrefix(strings.TrimSpace(res.Stdout), agentSHA256) {
		return nil
	}
	script := fmt.Sprintf("gunzip -c > %[1]s.new && chmod 0755 %[1]s.new && mv %[1]s.new %[1]s", agentPath)
	if err := b.mustRun(ctx, RunOptions{Stdin: agentGzip}, b.inDistro("sh", "-c", script)...); err != nil {
		return fmt.Errorf("install engine-agent: %w", err)
	}
	return nil
}
