package wsl

import (
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// The docker CLI on Windows connects to \\.\pipe\docker_engine by default, so
// listening there makes `docker` work without a context (F-04). If another
// engine already owns that name, fall back to a pipe named after the distro.
func pipeNames(distro string) []string {
	return []string{`\\.\pipe\docker_engine`, `\\.\pipe\` + distro}
}

// pipeSecurity allows only the current user and SYSTEM (no network access:
// named pipes here are local only, and remote clients are rejected by go-winio).
func pipeSecurity() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", user.User.Sid.String()), nil
}

func (b *Backend) startEndpoint() error {
	b.mu.Lock()
	running := b.endpoint != nil
	b.mu.Unlock()
	if running {
		return nil
	}
	sddl, err := pipeSecurity()
	if err != nil {
		return fmt.Errorf("pipe security: %w", err)
	}
	var (
		listener net.Listener
		name     string
		errs     []string
	)
	for _, n := range pipeNames(b.Distro) {
		l, err := winio.ListenPipe(n, &winio.PipeConfig{SecurityDescriptor: sddl})
		if err == nil {
			listener, name = l, n
			break
		}
		errs = append(errs, n+": "+err.Error())
	}
	if listener == nil {
		return fmt.Errorf("listen on a named pipe: %s", strings.Join(errs, "; "))
	}
	e := &endpoint{listener: listener, address: "npipe://" + strings.ReplaceAll(name, `\`, "/")}
	b.mu.Lock()
	b.endpoint = e
	b.mu.Unlock()
	e.wg.Add(1)
	go b.serve(e, metaWindows)
	return nil
}
