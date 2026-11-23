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

// Options change container create requests on their way to the engine.
type Options struct {
	// Translate rewrites bind mount paths (nil: no rewriting).
	Translate Translator
	// Env is added to new containers unless they set the same key (F-11).
	Env []string
	// BeforeCreate runs before a container create request is forwarded, for
	// example to make sure the paths it binds are mounted. Errors are logged
	// and the request goes on (the engine then reports what is missing).
	BeforeCreate func(ctx context.Context) error
}

// NewProxy returns a handler that forwards every request to the engine at
// socketPath and rewrites bind mount paths with translate. Streaming responses
// (logs, events, attach) and protocol upgrades (exec, attach) pass through.
func NewProxy(socketPath string, translate Translator, log *slog.Logger) http.Handler {
	return NewProxyWith(socketPath, Options{Translate: translate}, log)
}

// NewProxyWith is NewProxy with all options.
func NewProxyWith(socketPath string, opts Options, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	translate := opts.Translate
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
			if r.In.Method != http.MethodPost || !containerCreate.MatchString(r.In.URL.Path) {
				return
			}
			if opts.BeforeCreate != nil {
				if err := opts.BeforeCreate(r.In.Context()); err != nil {
					log.Warn("before container create", "err", err)
				}
			}
			if translate == nil && len(opts.Env) == 0 {
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Out.Body, maxCreateBody))
			_ = r.Out.Body.Close()
			if err != nil {
				log.Warn("read container create body", "err", err)
				r.Out.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			rewritten, err := rewriteCreate(body, translate, opts.Env)
			if err != nil {
				// Let the engine report the malformed request itself.
				log.Warn("rewrite container create", "err", err)
				rewritten = body
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

func rewriteCreate(body []byte, translate Translator, env []string) ([]byte, error) {
	if translate != nil {
		out, _, err := RewriteContainerCreate(body, translate)
		if err != nil {
			return nil, err
		}
		body = out
	}
	out, _, err := InjectEnv(body, env)
	return out, err
}
