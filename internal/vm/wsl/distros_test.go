package wsl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestIntegrationTargets(t *testing.T) {
	b := &Backend{Distro: "engine"}
	list := parseList("NAME STATE VERSION\n  Debian Running 2\n* Ubuntu Running 2\n  engine Running 2\n")
	if got := b.integrationTargets(list); !reflect.DeepEqual(got, []string{"Ubuntu"}) {
		t.Errorf("targets = %v, want the default distro", got)
	}
	for _, list := range []string{
		"NAME STATE VERSION\n* Legacy Running 1\n",         // WSL1 cannot share /mnt/wsl
		"NAME STATE VERSION\n* engine Running 2\n",         // the engine itself
		"NAME STATE VERSION\n* docker-desktop Running 2\n", // another tool's distro
	} {
		if got := b.integrationTargets(parseList(list)); got != nil {
			t.Errorf("%q: targets = %v, want none", list, got)
		}
	}
	b.Distros = []string{"Debian"}
	if got := b.integrationTargets(list); !reflect.DeepEqual(got, []string{"Debian"}) {
		t.Errorf("override ignored: %v", got)
	}
}

func TestStartIntegratesTheDefaultDistro(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.integrations) != 2 {
		t.Fatalf("integration commands = %v, want mount and context", f.integrations)
	}
	if !strings.Contains(f.integrations[0], "--user root") || !strings.Contains(f.integrations[0], "/mnt/wsl/test-engine/distros/Ubuntu") {
		t.Errorf("mount command = %q", f.integrations[0])
	}
	if strings.Contains(f.integrations[1], "--user root") || !strings.Contains(f.integrations[1], "/mnt/wsl/test-engine/run/Ubuntu/docker.sock") {
		t.Errorf("context command = %q (must run as the default user)", f.integrations[1])
	}
	// Later upkeep re-checks the mount but does not touch the context again.
	if err := b.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.integrations) != 3 || !strings.Contains(f.integrations[2], "mount --bind") {
		t.Errorf("after Maintain: %v", f.integrations)
	}
}
