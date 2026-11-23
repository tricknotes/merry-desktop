package wsl

import (
	"context"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestApplyNetwork(t *testing.T) {
	n := vm.Network{ProxyURL: "http://user:secret@p:3128", NoProxy: "corp.example"}
	stamp := ""
	var applied []map[string]string
	f := &fakeRunner{state: "Running"}
	f.handle = func(opts RunOptions, args []string) (Result, bool) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "cat "+networkStamp):
			if stamp == "" {
				return Result{ExitCode: 1}, true
			}
			return Result{Stdout: stamp + "\n"}, true
		case strings.Contains(joined, "engine-apply-network"):
			applied = append(applied, opts.Env)
			stamp = args[len(args)-1]
			if strings.Contains(joined, "secret") {
				t.Errorf("the proxy is on the command line: %s", joined)
			}
			return Result{}, true
		}
		return Result{}, false
	}
	b := newTestBackend(t, f)
	ctx := context.Background()

	// No stamp yet (an older VM): applied, but not reported as a change.
	if changed, err := b.ApplyNetwork(ctx, n); err != nil || changed || len(applied) != 1 || applied[0]["ENGINE_PROXY"] != n.ProxyURL {
		t.Fatalf("first: changed=%v err=%v applied=%v", changed, err, applied)
	}
	// Same settings, seen by a backend that has not applied them: the stamp in
	// the VM matches, nothing is applied.
	b2 := newTestBackend(t, f)
	if changed, _ := b2.ApplyNetwork(ctx, n); changed || len(applied) != 1 {
		t.Fatalf("same settings: changed=%v applied=%d", changed, len(applied))
	}
	// Changed: applied and reported.
	n.ProxyURL = ""
	if changed, err := b.ApplyNetwork(ctx, n); err != nil || !changed || len(applied) != 2 {
		t.Fatalf("changed: changed=%v err=%v applied=%d", changed, err, len(applied))
	}
}
