package engineapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strconv"
)

// containerCreate matches POST [/v1.xx]/containers/create.
var containerCreate = regexp.MustCompile(`^(/v[0-9.]+)?/containers/create$`)

// maxCreateBody bounds the body read for rewriting (create bodies are small).
const maxCreateBody = 8 << 20

// NewProxy returns a handler that forwards every request to the engine at
// socketPath and rewrites bind mount paths with translate. Streaming responses
// (logs, events, attach) and protocol upgrades (exec, attach) pass through.
func NewProxy(socketPath string, translate Translator, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
	}
	return &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // stream logs and events as they come
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = "docker"
			r.Out.Host = "docker"
			if translate == nil || r.In.Method != http.MethodPost || !containerCreate.MatchString(r.In.URL.Path) {
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Out.Body, maxCreateBody))
			_ = r.Out.Body.Close()
			if err != nil {
				log.Warn("read container create body", "err", err)
				r.Out.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			rewritten, changed, err := RewriteContainerCreate(body, translate)
			if err != nil {
				// Let the engine report the malformed request itself.
				log.Warn("rewrite container create", "err", err)
				rewritten = body
			} else if changed {
				log.Info("rewrote bind mount paths")
			}
			r.Out.Body = io.NopCloser(bytes.NewReader(rewritten))
			r.Out.ContentLength = int64(len(rewritten))
			r.Out.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
			r.Out.Header.Del("Transfer-Encoding")
			r.Out.TransferEncoding = nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn("engine request failed", "path", r.URL.Path, "err", err)
			http.Error(w, "container engine is not reachable: "+err.Error(), http.StatusBadGateway)
		},
	}
}
