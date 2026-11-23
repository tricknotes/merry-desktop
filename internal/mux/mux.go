// Package mux carries many byte streams over one connection, such as the
// standard input and output of a single wsl.exe process.
//
// Each frame is a 9-byte header (stream ID, type, payload length) followed by
// the payload. Streams have a receive window, so a slow reader cannot make the
// other side buffer without limit. Streams implement net.Conn, so net/http can
// serve and hijack them.
package mux

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	frameOpen   = 1 // payload: metadata from the opener
	frameData   = 2
	frameClose  = 3 // the sender will not write any more (half close)
	frameReset  = 4 // the sender has closed the stream completely
	frameWindow = 5 // payload: uint32 bytes the receiver has consumed

	headerSize = 9
	// MaxPayload is the largest data frame.
	MaxPayload = 32 * 1024
	// Window is how many unacknowledged bytes a stream may have in flight.
	Window = 256 * 1024
)

// ErrClosed is returned after the session or stream was closed.
var ErrClosed = errors.New("mux: closed")

// Session multiplexes streams over conn.
type Session struct {
	conn   io.ReadWriteCloser
	client bool

	wmu sync.Mutex
	w   *bufio.Writer

	mu      sync.Mutex
	streams map[uint32]*Stream
	nextID  uint32
	closed  bool
	err     error
	accept  chan *Stream
	done    chan struct{}
}

// Client starts a session that opens streams.
func Client(conn io.ReadWriteCloser) *Session { return newSession(conn, true) }

// Server starts a session that accepts streams.
func Server(conn io.ReadWriteCloser) *Session { return newSession(conn, false) }

func newSession(conn io.ReadWriteCloser, client bool) *Session {
	s := &Session{
		conn:    conn,
		client:  client,
		w:       bufio.NewWriterSize(conn, MaxPayload+headerSize),
		streams: map[uint32]*Stream{},
		nextID:  1,
		accept:  make(chan *Stream, 16),
		done:    make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// Done is closed when the session ends; Err then tells why.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Open starts a new stream. meta is delivered to the accepting side.
func (s *Session) Open(meta []byte) (*Stream, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	id := s.nextID
	s.nextID += 2
	st := newStream(s, id, meta)
	s.streams[id] = st
	s.mu.Unlock()
	if err := s.writeFrame(id, frameOpen, meta); err != nil {
		return nil, err
	}
	return st, nil
}

// Accept waits for a stream opened by the other side.
func (s *Session) Accept() (*Stream, error) {
	select {
	case st := <-s.accept:
		return st, nil
	case <-s.done:
		return nil, ErrClosed
	}
}

// Close ends the session and all its streams.
func (s *Session) Close() error {
	s.shutdown(ErrClosed)
	return s.conn.Close()
}

func (s *Session) shutdown(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.err = err
	streams := s.streams
	s.streams = map[uint32]*Stream{}
	s.mu.Unlock()
	for _, st := range streams {
		st.terminate()
	}
	close(s.done)
}

func (s *Session) writeFrame(id uint32, typ byte, payload []byte) error {
	var h [headerSize]byte
	binary.BigEndian.PutUint32(h[0:4], id)
	h[4] = typ
	binary.BigEndian.PutUint32(h[5:9], uint32(len(payload)))
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.w.Write(h[:]); err != nil {
		return err
	}
	if _, err := s.w.Write(payload); err != nil {
		return err
	}
	return s.w.Flush()
}

func (s *Session) readLoop() {
	r := bufio.NewReaderSize(s.conn, MaxPayload+headerSize)
	var h [headerSize]byte
	for {
		if _, err := io.ReadFull(r, h[:]); err != nil {
			s.shutdown(err)
			return
		}
		id := binary.BigEndian.Uint32(h[0:4])
		typ := h[4]
		n := binary.BigEndian.Uint32(h[5:9])
		if n > MaxPayload {
			s.shutdown(errors.New("mux: frame too large"))
			return
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			s.shutdown(err)
			return
		}
		s.dispatch(id, typ, payload)
	}
}

func (s *Session) dispatch(id uint32, typ byte, payload []byte) {
	s.mu.Lock()
	st := s.streams[id]
	if typ == frameOpen && st == nil && !s.closed {
		st = newStream(s, id, payload)
		s.streams[id] = st
		s.mu.Unlock()
		select {
		case s.accept <- st:
		case <-s.done:
		}
		return
	}
	s.mu.Unlock()
	if st == nil {
		return // late frame for a stream that is gone
	}
	switch typ {
	case frameData:
		st.receive(payload)
	case frameClose:
		st.remoteClose()
	case frameReset:
		st.terminate()
		s.forget(id)
	case frameWindow:
		if len(payload) == 4 {
			st.grant(int(binary.BigEndian.Uint32(payload)))
		}
	}
}

func (s *Session) forget(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.streams, id)
}

// Stream is one bidirectional byte stream.
type Stream struct {
	sess *Session
	id   uint32
	meta []byte

	mu           sync.Mutex
	cond         *sync.Cond
	buf          []byte
	consumed     int  // bytes read since the last window update
	remoteClosed bool // the peer will send no more data
	terminated   bool // reset by either side, or the session ended
	localClosed  bool // CloseWrite was called
	sendWindow   int
	readDeadline time.Time
	timer        *time.Timer
}

func newStream(s *Session, id uint32, meta []byte) *Stream {
	st := &Stream{sess: s, id: id, meta: meta, sendWindow: Window}
	st.cond = sync.NewCond(&st.mu)
	return st
}

// Meta returns the metadata given to Open.
func (st *Stream) Meta() []byte { return st.meta }

func (st *Stream) receive(p []byte) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.terminated {
		return
	}
	st.buf = append(st.buf, p...)
	st.cond.Broadcast()
}

func (st *Stream) remoteClose() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.remoteClosed = true
	st.cond.Broadcast()
}

func (st *Stream) terminate() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.terminated = true
	st.cond.Broadcast()
}

func (st *Stream) grant(n int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sendWindow += n
	st.cond.Broadcast()
}

// timeoutError is what Read returns after its deadline passed.
type timeoutError struct{}

func (timeoutError) Error() string   { return "mux: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
func (timeoutError) Unwrap() error   { return os.ErrDeadlineExceeded }

func (st *Stream) Read(p []byte) (int, error) {
	st.mu.Lock()
	for len(st.buf) == 0 {
		switch {
		case st.remoteClosed || st.terminated:
			st.mu.Unlock()
			return 0, io.EOF
		case !st.readDeadline.IsZero() && !time.Now().Before(st.readDeadline):
			st.mu.Unlock()
			return 0, timeoutError{}
		}
		st.cond.Wait()
	}
	n := copy(p, st.buf)
	st.buf = st.buf[n:]
	st.consumed += n
	update := 0
	if st.consumed >= Window/2 && !st.terminated {
		update, st.consumed = st.consumed, 0
	}
	st.mu.Unlock()
	if update > 0 {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(update))
		_ = st.sess.writeFrame(st.id, frameWindow, b[:])
	}
	return n, nil
}

func (st *Stream) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		st.mu.Lock()
		for st.sendWindow == 0 && !st.terminated {
			st.cond.Wait()
		}
		if st.terminated || st.localClosed {
			st.mu.Unlock()
			return written, ErrClosed
		}
		n := min(len(p), st.sendWindow, MaxPayload)
		st.sendWindow -= n
		st.mu.Unlock()
		if err := st.sess.writeFrame(st.id, frameData, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// CloseWrite tells the peer that no more data will be written (it reads EOF).
func (st *Stream) CloseWrite() error {
	st.mu.Lock()
	if st.localClosed || st.terminated {
		st.mu.Unlock()
		return nil
	}
	st.localClosed = true
	st.mu.Unlock()
	return st.sess.writeFrame(st.id, frameClose, nil)
}

// Close closes both directions and tells the peer to drop the stream.
func (st *Stream) Close() error {
	st.mu.Lock()
	if st.terminated {
		st.mu.Unlock()
		return nil
	}
	st.terminated = true
	if st.timer != nil {
		st.timer.Stop()
	}
	st.cond.Broadcast()
	st.mu.Unlock()
	st.sess.forget(st.id)
	return st.sess.writeFrame(st.id, frameReset, nil)
}

// SetReadDeadline makes a blocked Read return a timeout error at t. net/http
// uses a deadline in the past to abort its background read.
func (st *Stream) SetReadDeadline(t time.Time) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.readDeadline = t
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	if !t.IsZero() {
		st.timer = time.AfterFunc(time.Until(t), func() {
			st.mu.Lock()
			st.cond.Broadcast()
			st.mu.Unlock()
		})
	}
	st.cond.Broadcast()
	return nil
}

// SetWriteDeadline is accepted but not enforced; writes only block on the window.
func (st *Stream) SetWriteDeadline(time.Time) error { return nil }

func (st *Stream) SetDeadline(t time.Time) error { return st.SetReadDeadline(t) }

type addr string

func (a addr) Network() string { return "mux" }
func (a addr) String() string  { return string(a) }

func (st *Stream) LocalAddr() net.Addr  { return addr("local") }
func (st *Stream) RemoteAddr() net.Addr { return addr("remote") }

var _ net.Conn = (*Stream)(nil)

// Listener adapts a server Session to net.Listener, for http.Server.
type Listener struct{ Session *Session }

func (l Listener) Accept() (net.Conn, error) {
	st, err := l.Session.Accept()
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (l Listener) Close() error   { return l.Session.Close() }
func (l Listener) Addr() net.Addr { return addr("mux") }
