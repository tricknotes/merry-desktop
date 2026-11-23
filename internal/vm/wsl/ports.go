package wsl

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/engineapi"
)

// hostListener is a port something listens on, on the Windows host.
type hostListener struct {
	Proto   string
	Port    int
	PID     int
	Process string
}

// Windows forwards the ports published in WSL2 to its own localhost through
// wslrelay.exe. When another Windows program already listens on the port, the
// container still starts but is not reachable from Windows; nothing reports
// it. portWarnings finds those ports (F-22).
func (b *Backend) portWarnings(ctx context.Context, ports []engineapi.PortRequest) []string {
	listeners, err := b.listHostPorts(ctx)
	if err != nil {
		return nil // best effort: never block or fail a container create
	}
	byPort := map[string]hostListener{}
	for _, l := range listeners {
		byPort[fmt.Sprintf("%s/%d", l.Proto, l.Port)] = l
	}
	var warnings []string
	for _, p := range ports {
		l, ok := byPort[fmt.Sprintf("%s/%d", p.Proto, p.Port)]
		if !ok {
			continue
		}
		owner := fmt.Sprintf("PID %d", l.PID)
		if l.Process != "" {
			owner = fmt.Sprintf("%s、PID %d", l.Process, l.PID)
		}
		if strings.EqualFold(l.Process, "wslrelay.exe") {
			owner = "WSL の中のほかのサービス"
		}
		warnings = append(warnings, fmt.Sprintf(
			"ポート %d/%s は Windows で既に使われています(%s)。コンテナは起動しますが、Windows の localhost:%d からはこのコンテナにつながりません。",
			p.Port, p.Proto, owner, p.Port))
	}
	return warnings
}

func (b *Backend) listHostPorts(ctx context.Context) ([]hostListener, error) {
	if b.HostPorts != nil {
		return b.HostPorts(ctx)
	}
	var all []hostListener
	for _, proto := range []string{"tcp", "udp"} {
		out, err := hostOutput(ctx, "netstat.exe", "-ano", "-p", proto)
		if err != nil {
			return nil, err
		}
		all = append(all, parseNetstat(out, proto)...)
	}
	names := map[int]string{}
	for i, l := range all {
		if _, ok := names[l.PID]; !ok {
			names[l.PID] = processName(ctx, l.PID)
		}
		all[i].Process = names[l.PID]
	}
	return all, nil
}

// parseNetstat reads `netstat -ano -p tcp|udp`. Headers are localized, so
// only data lines are used: "TCP 0.0.0.0:135 0.0.0.0:0 LISTENING 2024" and
// "UDP 0.0.0.0:5353 *:* 3260".
func parseNetstat(out, proto string) []hostListener {
	var ls []hostListener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.EqualFold(f[0], proto) {
			continue
		}
		if proto == "tcp" && (len(f) < 5 || f[3] != "LISTENING") {
			continue
		}
		i := strings.LastIndex(f[1], ":")
		port, err1 := strconv.Atoi(f[1][i+1:])
		pid, err2 := strconv.Atoi(f[len(f)-1])
		if i < 0 || err1 != nil || err2 != nil {
			continue
		}
		ls = append(ls, hostListener{Proto: proto, Port: port, PID: pid})
	}
	return ls
}

// processName returns the image name of a Windows process, or "".
func processName(ctx context.Context, pid int) string {
	out, err := hostOutput(ctx, "tasklist.exe", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	if err != nil {
		return ""
	}
	rec, err := csv.NewReader(strings.NewReader(out)).Read()
	if err != nil || len(rec) < 2 || rec[1] != strconv.Itoa(pid) {
		return ""
	}
	return rec[0]
}

func hostOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out, err := cmd.Output()
	return strings.ReplaceAll(string(out), "\r", ""), err
}
