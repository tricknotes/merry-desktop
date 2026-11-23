package mux

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

func pair(t *testing.T) (*Session, *Session) {
	t.Helper()
	a, b := net.Pipe()
	client, server := Client(a), Server(b)
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

// echo copies every accepted stream back to itself, then half-closes.
func echo(server *Session) {
	for {
		st, err := server.Accept()
		if err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(st, st)
			_ = st.CloseWrite()
		}()
	}
}

func TestManyStreamsLargerThanTheWindow(t *testing.T) {
	client, server := pair(t)
	go echo(server)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := client.Open([]byte{byte(i)})
			if err != nil {
				t.Error(err)
				return
			}
			want := make([]byte, 3*Window+123)
			_, _ = rand.Read(want)
			go func() {
				_, _ = st.Write(want)
				_ = st.CloseWrite()
			}()
			got, err := io.ReadAll(st)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("stream %d: %d bytes, err %v (want %d bytes back)", i, len(got), err, len(want))
			}
			_ = st.Close()
		}()
	}
	wg.Wait()
}

func TestMetaIsDelivered(t *testing.T) {
	client, server := pair(t)
	if _, err := client.Open([]byte("windows")); err != nil {
		t.Fatal(err)
	}
	st, err := server.Accept()
	if err != nil || string(st.Meta()) != "windows" {
		t.Fatalf("Accept = %q, %v", st.Meta(), err)
	}
}

func TestReadDeadlineInThePastUnblocksRead(t *testing.T) {
	client, server := pair(t)
	go func() { _, _ = server.Accept() }()
	st, _ := client.Open(nil)
	errc := make(chan error, 1)
	go func() {
		_, err := st.Read(make([]byte, 1))
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = st.SetReadDeadline(time.Unix(1, 0))
	select {
	case err := <-errc:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("Read error = %v, want a timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read still blocked")
	}
	// After clearing the deadline the stream works again.
	_ = st.SetReadDeadline(time.Time{})
}

func TestCloseResetsThePeer(t *testing.T) {
	client, server := pair(t)
	st, _ := client.Open(nil)
	peer, _ := server.Accept()
	_ = st.Close()
	if _, err := io.ReadAll(peer); err != nil {
		t.Fatalf("peer read after reset: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := peer.Write([]byte("x")); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("peer can still write to a reset stream")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSessionCloseEndsStreams(t *testing.T) {
	client, server := pair(t)
	st, _ := client.Open(nil)
	_, _ = server.Accept()
	_ = server.Close()
	<-client.Done()
	if _, err := st.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("Read after session end = %v, want EOF", err)
	}
	if _, err := client.Open(nil); err == nil {
		t.Fatal("Open on a closed session succeeded")
	}
}

func TestHTTPOverMux(t *testing.T) {
	client, server := pair(t)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte(r.URL.Path+":"), body...))
	})}
	go func() { _ = srv.Serve(Listener{Session: server}) }()
	t.Cleanup(func() { _ = srv.Close() })

	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return client.Open(nil) },
	}}
	for i := range 5 { // reuses the kept-alive stream
		resp, err := hc.Post("http://engine/v1.57/_ping", "text/plain", bytes.NewReader([]byte{byte('a' + i)}))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if want := "/v1.57/_ping:" + string(rune('a'+i)); string(got) != want {
			t.Fatalf("response %q, want %q", got, want)
		}
	}
}
