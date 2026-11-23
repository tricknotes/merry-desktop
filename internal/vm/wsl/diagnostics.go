package wsl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

var _ vm.Diagnoser = (*Backend)(nil)

const agentLog = "/var/log/engine-agent.log"

// networkScript checks DNS and HTTPS reachability from inside the VM, through
// the configured proxy. It prints only status codes and timings.
const networkScript = `. /etc/engine/proxy.env 2>/dev/null
if [ -n "${HTTPS_PROXY:-}" ]; then echo "proxy: set"; else echo "proxy: none"; fi
echo "--- /etc/resolv.conf"; cat /etc/resolv.conf
for host in "$@"; do
  echo "--- $host"
  nslookup "$host" 2>&1 | tail -n 3
  curl -sS -o /dev/null -m 15 -w "https: %{http_code} in %{time_total}s\n" "https://$host/" 2>&1
done`

// Diagnostics gathers host facts (WSL version, distros, .wslconfig) and, when
// the VM runs, engine facts, logs and a network check from inside the VM.
// Sensitive files (proxy.env, the user's docker config) are never read.
func (b *Backend) Diagnostics(ctx context.Context, checkHosts []string) ([]vm.DiagnosticFile, error) {
	var files []vm.DiagnosticFile
	add := func(name string, data []byte) { files = append(files, vm.DiagnosticFile{Name: name, Data: data}) }
	host := func(name string, args ...string) {
		res, err := b.Runner.Run(ctx, args...)
		if err != nil {
			add(name, []byte("error: "+err.Error()))
			return
		}
		add(name, []byte(fmt.Sprintf("exit %d\n%s%s", res.ExitCode, res.Stdout, res.Stderr)))
	}
	host("host/wsl-version.txt", "--version")
	host("host/wsl-status.txt", "--status")
	host("host/wsl-list.txt", "--list", "--verbose")
	for name, path := range map[string]string{"host/wslconfig.txt": b.ConfigPath, "host/wsl-state.json": b.StatePath} {
		if data, err := os.ReadFile(path); err == nil {
			add(name, data)
		} else {
			add(name, []byte("not present: "+err.Error()))
		}
	}
	add("host/endpoint.txt", []byte("host endpoint: "+b.HostEndpoint()+"\n"))

	d, err := b.lookup(ctx)
	if err != nil || d.State != stateRunning {
		add("vm/NOT-RUNNING.txt", []byte("The VM was not running, so engine logs were not collected.\n"))
		return files, nil
	}
	vmCmd := func(name string, args ...string) {
		res, err := b.Runner.Run(ctx, b.inDistro(args...)...)
		if err != nil {
			add(name, []byte("error: "+err.Error()))
			return
		}
		add(name, []byte(fmt.Sprintf("exit %d\n%s%s", res.ExitCode, res.Stdout, res.Stderr)))
	}
	vmCmd("vm/docker-version.txt", "docker", "version")
	vmCmd("vm/docker-info.txt", "docker", "info")
	vmCmd("vm/dockerd.log", "tail", "-n", "1000", "/var/log/dockerd.log")
	vmCmd("vm/engine-agent.log", "tail", "-n", "1000", agentLog)
	vmCmd("vm/daemon.json", "cat", "/etc/docker/daemon.json")
	vmCmd("vm/wsl.conf", "cat", "/etc/wsl.conf")
	vmCmd("vm/ca-certificates.txt", "ls", "-l", "/usr/local/share/ca-certificates")
	vmCmd("vm/processes.txt", "ps", "-o", "pid,user,etime,args")
	vmCmd("vm/disk.txt", "df", "-h")

	hosts := append([]string{"registry-1.docker.io", "dl-cdn.alpinelinux.org"}, checkHosts...)
	vmCmd("vm/network.txt", append([]string{"sh", "-c", networkScript, "sh"}, sanitizeHosts(hosts)...)...)
	return files, nil
}

// sanitizeHosts keeps only host[:port] values, so nothing odd reaches the shell.
func sanitizeHosts(hosts []string) []string {
	var out []string
	for _, h := range hosts {
		if h != "" && strings.Trim(h, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-:") == "" {
			out = append(out, h)
		}
	}
	return out
}
