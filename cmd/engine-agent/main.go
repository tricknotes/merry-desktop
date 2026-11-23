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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"

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

func serveStdio(args []string) error {
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	socket := fs.String("engine-socket", "/var/run/docker.sock", "Docker Engine socket")
	distroRoot := fs.String("distro-root", "", "where other WSL distros' roots are visible (for \\\\wsl$ paths)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("engine-agent serving", "version", version, "socket", *socket)
	// Only the protocol may go to stdout; everything else goes to stderr.
	return serve(stdio{Reader: os.Stdin, Writer: os.Stdout}, *socket, *distroRoot, log)
}

// serve relays engine API requests arriving as mux streams on conn until the
// host closes the session.
func serve(conn io.ReadWriteCloser, socket, distroRoot string, log *slog.Logger) error {
	session := mux.Server(conn)
	windows := engineapi.NewProxy(socket, engineapi.WindowsPaths(distroRoot), log)
	plain := engineapi.NewProxy(socket, nil, log)

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
