package wsl

import (
	"io"
	"net"
	"sync"
)

// endpoint accepts docker CLI connections on the host and relays each one
// to the engine through the agent session.
type endpoint struct {
	listener net.Listener
	// Address is what a docker context uses, e.g. npipe:////./pipe/docker_engine.
	address string
	wg      sync.WaitGroup
}

// Client kinds sent as stream metadata (see cmd/engine-agent).
const metaWindows = "windows"

func (b *Backend) serve(e *endpoint, meta string) {
	defer e.wg.Done()
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			return
		}
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			relay(conn, b, meta)
		}()
	}
}

func relay(client net.Conn, b *Backend, meta string) {
	defer client.Close()
	engine, err := b.DialEngine(meta)
	if err != nil {
		return
	}
	defer engine.Close()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(engine, client)
		if cw, ok := engine.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		close(done)
	}()
	_, _ = io.Copy(client, engine)
	_ = client.Close() // unblocks the other copy
	<-done
}

// HostEndpoint is the docker host address for the host CLI ("" when stopped).
func (b *Backend) HostEndpoint() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.endpoint == nil {
		return ""
	}
	return b.endpoint.address
}

func (b *Backend) stopEndpoint() {
	b.mu.Lock()
	e := b.endpoint
	b.endpoint = nil
	b.mu.Unlock()
	if e != nil {
		_ = e.listener.Close()
	}
}
