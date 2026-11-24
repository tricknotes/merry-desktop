package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// Idle stop (F-06): when no container has run and no API request has come for
// a while, the agent stops the engine; the next request starts it again before
// it is forwarded. Every client (the host CLI and the integrated distros)
// reaches the engine through this agent, so all of them wake it. The agent
// itself keeps running, which also keeps its sockets.

type engineState int

const (
	engineAwake engineState = iota
	engineStopping
	engineAsleep
	engineStarting
)

// idleStop tracks the requests in flight and stops or starts the engine.
type idleStop struct {
	after time.Duration
	// start starts the engine and returns once it answers; stop stops it.
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
	// runningContainers counts the running containers.
	runningContainers func(ctx context.Context) (int, error)
	// notify tells the host about a change (asleep: the engine is stopped).
	notify func(asleep bool)
	now    func() time.Time
	log    *slog.Logger

	mu       sync.Mutex
	cond     *sync.Cond
	state    engineState
	inflight int
	// busy is when a request ended or a container ran last.
	busy time.Time
}

func newIdleStop(after time.Duration, log *slog.Logger) *idleStop {
	m := &idleStop{after: after, now: time.Now, log: log}
	m.cond = sync.NewCond(&m.mu)
	m.busy = m.now()
	return m
}

// acquire starts the engine if it is stopped and counts a request in flight.
// Call release when the request is done.
func (m *idleStop) acquire(ctx context.Context) error {
	m.mu.Lock()
	for m.state == engineStopping || m.state == engineStarting {
		m.cond.Wait()
	}
	if m.state == engineAsleep {
		m.state = engineStarting
		m.mu.Unlock()
		m.log.Info("starting the engine for a request")
		// Not the request's context: when that client gives up, the others
		// waiting still need the engine.
		err := m.start(context.WithoutCancel(ctx))
		m.mu.Lock()
		if err != nil {
			m.state = engineAsleep
			m.cond.Broadcast()
			m.mu.Unlock()
			m.log.Error("start the engine", "err", err)
			return err
		}
		m.state = engineAwake
		m.busy = m.now()
		m.cond.Broadcast()
		m.mu.Unlock()
		m.notify(false)
		m.mu.Lock()
	}
	m.inflight++
	m.mu.Unlock()
	return nil
}

func (m *idleStop) release() {
	m.mu.Lock()
	m.inflight--
	m.busy = m.now()
	m.mu.Unlock()
}

// check stops the engine if it has been idle long enough. It is called
// periodically.
func (m *idleStop) check(ctx context.Context) {
	if !m.idleLongEnough() {
		return
	}
	n, err := m.runningContainers(ctx)
	if err != nil || n > 0 {
		m.mu.Lock()
		m.busy = m.now()
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	if m.state != engineAwake || m.inflight > 0 || m.now().Sub(m.busy) < m.after {
		m.mu.Unlock()
		return
	}
	m.state = engineStopping
	m.mu.Unlock()

	// Tell the host first, so that it does not take the stopping engine for
	// a broken one.
	m.notify(true)
	m.log.Info("stopping the idle engine", "idle", m.after)
	err = m.stop(ctx)
	m.mu.Lock()
	if err != nil {
		m.state = engineAwake
		m.busy = m.now()
	} else {
		m.state = engineAsleep
	}
	m.cond.Broadcast()
	m.mu.Unlock()
	if err != nil {
		m.log.Error("stop the engine", "err", err)
		m.notify(false)
	}
}

func (m *idleStop) idleLongEnough() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == engineAwake && m.inflight == 0 && m.now().Sub(m.busy) >= m.after
}

// run calls check until ctx ends.
func (m *idleStop) run(ctx context.Context) {
	every := min(m.after/4, 30*time.Second)
	every = max(every, time.Second)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.check(ctx)
		}
	}
}

// wrap makes h wake the engine first and count as activity until it returns
// (for upgraded connections such as exec, until the connection ends).
func (m *idleStop) wrap(h http.Handler) http.Handler {
	if m == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.acquire(r.Context()); err != nil {
			http.Error(w, "the container engine could not be started: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer m.release()
		h.ServeHTTP(w, r)
	})
}

// The engine in the VM: started by engine-start (also the distro's boot
// command), stopped through its pid file.
const (
	engineStartCommand = "/usr/local/sbin/engine-start"
	enginePidFile      = "/var/run/docker.pid"
)

// startEngine runs the start script and waits until the engine answers.
func startEngine(ctx context.Context, socket string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	// No output is captured: the script leaves jobs in the background, and a
	// pipe would make this wait for them. It logs to /var/log/dockerd.log.
	if err := exec.CommandContext(ctx, engineStartCommand).Run(); err != nil {
		return fmt.Errorf("%s: %w", engineStartCommand, err)
	}
	client := engineClient(socket)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the engine did not answer: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// countRunning asks the engine how many containers are running.
func countRunning(ctx context.Context, socket string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json", nil)
	res, err := engineClient(socket).Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("list containers: %s", res.Status)
	}
	var list []json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return 0, err
	}
	return len(list), nil
}

func engineClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
}
