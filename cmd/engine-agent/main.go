// Command engine-agent runs inside the engine VM (Linux). It relays the Docker
// Engine API to the host over one multiplexed connection and rewrites host
// paths in bind mounts (F-04).
//
//	engine-agent stdio [flags]   serve over standard input/output
//	engine-agent version
//
// The host starts `wsl.exe ... --exec engine-agent stdio`; that session also
// keeps the WSL distro running.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tricknotes/merry-desktop/internal/engineapi"
	"github.com/tricknotes/merry-desktop/internal/mux"
)

// version is set by the build (-ldflags -X); the host compares checksums, not this.
var version = "dev"

// Stream metadata names the kind of client, which decides the path translation.
const metaWindows = "windows"

type streamKey struct{}

// stdio joins standard input and output into one connection.
type stdio struct {
	io.Reader
	io.Writer
}

func (stdio) Close() error {
	_ = os.Stdin.Close()
	return os.Stdout.Close()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: engine-agent stdio|version")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "stdio":
		if err := serveStdio(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "engine-agent:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(2)
	}
}

// names collects a repeated flag.
type names []string

func (n *names) String() string     { return strings.Join(*n, ",") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

func serveStdio(args []string) error {
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	socket := fs.String("engine-socket", "/var/run/docker.sock", "Docker Engine socket")
	distroRoot := fs.String("distro-root", "", "where other WSL distros' roots are bind-mounted")
	runDir := fs.String("run-dir", "", "directory for the per-distro sockets (under /mnt/wsl)")
	proxyEnv := fs.String("proxy-env", "", "file with the proxy settings (export KEY=VALUE) to pass to new containers")
	logFile := fs.String("log-file", "", "append logs to this file instead of stderr (the host discards stderr)")
	var distros names
	fs.Var(&distros, "distro", "serve a socket for this WSL distro (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var logOut io.Writer = os.Stderr
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		defer f.Close()
		logOut = f
	}
	log := slog.New(slog.NewTextHandler(logOut, nil))
	log.Info("engine-agent serving", "version", version, "socket", *socket, "distros", distros.String())
	env := containerProxyEnv(*proxyEnv, log)

	// Only the protocol may go to stdout; everything else goes to stderr.
	session := mux.Server(stdio{Reader: os.Stdin, Writer: os.Stdout})
	var servers []*http.Server
	for _, d := range distros {
		ensure := func(ctx context.Context) error {
			return ensureMounted(ctx, session, filepath.Join(*distroRoot, d), d)
		}
		srv, err := serveDistro(*runDir, d, *socket, *distroRoot, env, ensure, log)
		if err != nil {
			log.Error("distro socket", "distro", d, "err", err)
			continue
		}
		servers = append(servers, srv)
	}
	defer func() {
		for _, s := range servers {
			_ = s.Close()
		}
	}()
	return serve(session, *socket, *distroRoot, env, log)
}

// ensureMounted makes sure the distro's root is bind-mounted at path. The bind
// lives in the distro's own mount namespace, so it disappears when the distro
// restarts; then the host (which can run commands in that distro) is asked to
// mount it again before the request goes on.
func ensureMounted(ctx context.Context, session *mux.Session, path, distro string) error {
	mounted, err := isMountpoint("/proc/self/mountinfo", path)
	if err != nil || mounted {
		return err
	}
	st, err := session.Open([]byte(controlMount + distro))
	if err != nil {
		return err
	}
	defer st.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = st.SetReadDeadline(deadline)
	line, err := bufio.NewReader(st).ReadString('\n')
	if err != nil {
		return fmt.Errorf("ask the host to mount %s: %w", distro, err)
	}
	if line = strings.TrimSpace(line); line != "ok" {
		return fmt.Errorf("mount %s: %s", distro, line)
	}
	return nil
}

// controlMount is the stream metadata the agent uses to ask the host to
// bind-mount a distro's root (the host answers "ok" or "error: ...").
const controlMount = "mount:"

// isMountpoint reports whether path is a mount point, per a mountinfo file.
func isMountpoint(mountinfo, path string) (bool, error) {
	data, err := os.ReadFile(mountinfo)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && unescapeMountPath(fields[4]) == path {
			return true, nil
		}
	}
	return false, nil
}

// unescapeMountPath undoes the octal escapes of mountinfo (\040 for a space).
func unescapeMountPath(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// proxyKeys are the variables passed to containers, like the docker CLI's
// "proxies" setting does.
var proxyKeys = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
}

// containerProxyEnv reads the proxy settings written by apply-network.sh. A
// proxy URL with credentials is not passed on: container environment variables
// are visible to anyone who can run `docker inspect`.
func containerProxyEnv(path string, log *slog.Logger) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("read proxy settings", "err", err)
		}
		return nil
	}
	var env []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || !proxyKeys[k] || v == "" {
			continue
		}
		if u, err := url.Parse(v); err == nil && u.User != nil {
			log.Warn("the proxy has credentials; it is not passed to containers", "key", k)
			return nil
		}
		env = append(env, k+"="+v)
	}
	return env
}

// serveDistro listens on <runDir>/<distro>/docker.sock for the docker CLI in
// that WSL distro and rewrites its paths to where its root is bind-mounted.
// The socket is world-accessible on purpose: /mnt/wsl is reachable only from
// this Windows user's WSL distros, which can run as root in the VM anyway
// (docs/phase0-wsl2-backend.md, finding 4).
func serveDistro(runDir, distro, socket, distroRoot string, env []string, ensureMount func(context.Context) error, log *slog.Logger) (*http.Server, error) {
	if runDir == "" || distro == "" || strings.ContainsAny(distro, "/\\") {
		return nil, fmt.Errorf("invalid run dir or distro name %q", distro)
	}
	dir := filepath.Join(runDir, distro)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "docker.sock")
	_ = os.Remove(path) // left over from a previous run
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = l.Close()
		return nil, err
	}
	srv := &http.Server{
		Handler: engineapi.NewProxyWith(socket, engineapi.Options{
			Translate: engineapi.DistroPaths(distroRoot, distro), Env: env, BeforeCreate: ensureMount,
		}, log),
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	go func() { _ = srv.Serve(l) }()
	return srv, nil
}

// serve relays engine API requests arriving as mux streams on conn until the
// host closes the session.
func serve(session *mux.Session, socket, distroRoot string, env []string, log *slog.Logger) error {
	windows := engineapi.NewProxyWith(socket, engineapi.Options{Translate: engineapi.WindowsPaths(distroRoot), Env: env}, log)
	plain := engineapi.NewProxyWith(socket, engineapi.Options{Env: env}, log)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if st, _ := r.Context().Value(streamKey{}).(*mux.Stream); st != nil && string(st.Meta()) == metaWindows {
				windows.ServeHTTP(w, r)
				return
			}
			plain.ServeHTTP(w, r)
		}),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if st, ok := c.(*mux.Stream); ok {
				return context.WithValue(ctx, streamKey{}, st)
			}
			return ctx
		},
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	go func() {
		<-session.Done()
		_ = srv.Close()
	}()
	err := srv.Serve(mux.Listener{Session: session})
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, mux.ErrClosed) {
		return nil // the host closed the session
	}
	return err
}
