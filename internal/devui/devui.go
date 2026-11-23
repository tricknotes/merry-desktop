// Package devui serves the app's windows to a browser for development. The
// frontend talks to the same appservice.Service as in the app, over HTTP
// instead of Wails bindings:
//
//	POST /api/call/<Method>   body: JSON array of arguments -> JSON result
//	GET  /api/events          Server-Sent Events: {"name": ..., "data": ...}
//
// It listens on the loopback interface only. Every API request must carry the
// random token (header X-Merry-Token, or ?token= for the event stream) and a
// loopback Host header, so that other local processes and web pages cannot
// drive the VM.
package devui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
)

// Server serves the frontend and the API.
type Server struct {
	Token string

	service reflect.Value
	assets  fs.FS

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// New returns a server for service (a pointer whose exported methods form the
// API) and the built frontend assets (frontend/dist).
func New(service any, assets fs.FS) (*Server, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	return &Server{
		Token:   hex.EncodeToString(token),
		service: reflect.ValueOf(service),
		assets:  assets,
		clients: map[chan []byte]struct{}{},
	}, nil
}

// Emit sends an event to every connected window. It never blocks: a client
// that is too slow loses events.
func (s *Server) Emit(name string, data any) {
	msg, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(s.assets))
	mux.HandleFunc("POST /api/call/{method}", s.guard(s.call))
	mux.HandleFunc("GET /api/events", s.guard(s.events))
	return mux
}

// guard rejects requests without the token or with a non-loopback Host (DNS
// rebinding: a web page could otherwise reach the server under its own name).
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		token := r.Header.Get("X-Merry-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
			http.Error(w, "missing or wrong token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

var errorType = reflect.TypeFor[error]()

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("method")
	m := s.service.MethodByName(name)
	if !m.IsValid() || name == "" || strings.ToUpper(name[:1]) != name[:1] {
		http.Error(w, "unknown method", http.StatusNotFound)
		return
	}
	var raw []json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		http.Error(w, "body must be a JSON array of arguments", http.StatusBadRequest)
		return
	}
	t := m.Type()
	if len(raw) != t.NumIn() {
		http.Error(w, fmt.Sprintf("%s takes %d arguments", name, t.NumIn()), http.StatusBadRequest)
		return
	}
	args := make([]reflect.Value, len(raw))
	for i := range raw {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
			http.Error(w, fmt.Sprintf("argument %d: %v", i, err), http.StatusBadRequest)
			return
		}
		args[i] = v.Elem()
	}

	var result any
	for _, out := range m.Call(args) {
		if out.Type().Implements(errorType) {
			if !out.IsNil() {
				// The message is shown to the user, like a rejected Wails call.
				http.Error(w, out.Interface().(error).Error(), http.StatusUnprocessableEntity)
				return
			}
			continue
		}
		result = out.Interface()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := make(chan []byte, 32)
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
	}()
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-c:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
