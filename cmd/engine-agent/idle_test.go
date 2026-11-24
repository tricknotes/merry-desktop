package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeEngine records what the idle stop did.
type fakeEngine struct {
	mu       sync.Mutex
	starts   int
	stops    int
	running  int
	failStop bool
	events   []bool
}

func newTestIdle(e *fakeEngine, clock *time.Time) *idleStop {
	m := newIdleStop(10*time.Minute, slog.New(slog.DiscardHandler))
	m.now = func() time.Time { return *clock }
	m.busy = *clock
	m.start = func(context.Context) error { e.mu.Lock(); e.starts++; e.mu.Unlock(); return nil }
	m.stop = func(context.Context) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.failStop {
			return errors.New("boom")
		}
		e.stops++
		return nil
	}
	m.runningContainers = func(context.Context) (int, error) { return e.running, nil }
	m.notify = func(asleep bool) { e.mu.Lock(); e.events = append(e.events, asleep); e.mu.Unlock() }
	return m
}

func TestIdleStopsAfterTheTimeAndWakesOnRequest(t *testing.T) {
	e := &fakeEngine{}
	clock := time.Unix(1000, 0)
	m := newTestIdle(e, &clock)
	ctx := context.Background()

	clock = clock.Add(9 * time.Minute)
	m.check(ctx)
	if e.stops != 0 {
		t.Fatal("stopped before the idle time")
	}
	clock = clock.Add(2 * time.Minute)
	m.check(ctx)
	if e.stops != 1 || m.state != engineAsleep {
		t.Fatalf("stops = %d, state = %d", e.stops, m.state)
	}

	h := m.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_ping", nil))
	if rec.Code != http.StatusNoContent || e.starts != 1 || m.state != engineAwake {
		t.Fatalf("code = %d, starts = %d, state = %d", rec.Code, e.starts, m.state)
	}
	if len(e.events) != 2 || !e.events[0] || e.events[1] {
		t.Fatalf("events = %v, want [asleep awake]", e.events)
	}
	// The request counts as activity: no stop right after it.
	m.check(ctx)
	if e.stops != 1 {
		t.Fatal("stopped right after a request")
	}
}

func TestIdleDoesNotStopWithContainersOrRequests(t *testing.T) {
	e := &fakeEngine{running: 1}
	clock := time.Unix(1000, 0)
	m := newTestIdle(e, &clock)
	ctx := context.Background()

	clock = clock.Add(time.Hour)
	m.check(ctx)
	if e.stops != 0 {
		t.Fatal("stopped with a running container")
	}
	// The container stops on its own: the idle time counts from the last check
	// that saw it.
	e.running = 0
	clock = clock.Add(5 * time.Minute)
	m.check(ctx)
	if e.stops != 0 {
		t.Fatal("stopped too early after the container ended")
	}

	// A long request (docker events, an exec session) keeps it awake.
	if err := m.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	m.check(ctx)
	if e.stops != 0 {
		t.Fatal("stopped during a request")
	}
	m.release()
	clock = clock.Add(11 * time.Minute)
	m.check(ctx)
	if e.stops != 1 {
		t.Fatalf("stops = %d after the request ended and the idle time passed", e.stops)
	}
}

func TestIdleStopFailureKeepsTheEngineAwake(t *testing.T) {
	e := &fakeEngine{failStop: true}
	clock := time.Unix(1000, 0)
	m := newTestIdle(e, &clock)
	clock = clock.Add(time.Hour)
	m.check(context.Background())
	if m.state != engineAwake || len(e.events) != 0 {
		t.Fatalf("state = %d, events = %v", m.state, e.events)
	}
}

func TestIdleWrapNilPassesThrough(t *testing.T) {
	var m *idleStop
	called := false
	h := m.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler not called")
	}
}

func TestIdleRequestsWaitForAStop(t *testing.T) {
	e := &fakeEngine{}
	clock := time.Unix(1000, 0)
	m := newTestIdle(e, &clock)
	release := make(chan struct{})
	m.stop = func(context.Context) error { <-release; return nil }
	clock = clock.Add(time.Hour)
	go m.check(context.Background())
	for {
		m.mu.Lock()
		s := m.state
		m.mu.Unlock()
		if s == engineStopping {
			break
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan error)
	go func() { done <- m.acquire(context.Background()) }()
	select {
	case <-done:
		t.Fatal("a request went on while the engine was stopping")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if e.starts != 1 {
		t.Fatalf("starts = %d, want the engine started again", e.starts)
	}
	m.release()
}
