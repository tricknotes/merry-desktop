package wsl

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrationsListsCandidates(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	got, err := b.Integrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// listOutput has Ubuntu (default, WSL2) and the engine itself.
	if len(got) != 1 || got[0].Name != "Ubuntu" || !got[0].Default || !got[0].Enabled {
		t.Fatalf("integrations = %+v", got)
	}
	_ = b.SetIntegrations(context.Background(), []string{})
	got, _ = b.Integrations(context.Background())
	if got[0].Enabled {
		t.Errorf("Ubuntu still enabled after choosing none: %+v", got)
	}
}

func TestSetIntegrationsReconnectsWithoutStoppingTheVM(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.SetIntegrations(ctx, []string{}); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 2 {
		t.Fatalf("agent sessions = %d, want 2 (reconnected)", len(f.started))
	}
	if last := f.calls[len(f.calls)-1]; !strings.Contains(last, "umount") || !strings.Contains(last, "--distribution Ubuntu") {
		t.Errorf("last call = %q, want the Ubuntu bind mount removed", last)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "--terminate") || c == "--shutdown" {
			t.Errorf("stopped the VM: %q", c)
		}
	}
	var lastStart string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "start: ") {
			lastStart = c
		}
	}
	if strings.Contains(lastStart, "--distro Ubuntu") {
		t.Errorf("the new agent still serves Ubuntu: %q", lastStart)
	}
}
