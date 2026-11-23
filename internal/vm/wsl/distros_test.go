package wsl

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestAgentCanAskForARemount(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(f.integrations)
	ask := func(meta string) string {
		st, err := f.agents[0].Open([]byte(meta))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		buf := make([]byte, 128)
		n, _ := io.ReadFull(st, buf[:1])
		m, _ := st.Read(buf[n:])
		return strings.TrimSpace(string(buf[:n+m]))
	}
	if got := ask(controlMount + "Ubuntu"); got != "ok" {
		t.Fatalf("reply = %q", got)
	}
	if len(f.integrations) != before+1 || !strings.Contains(f.integrations[before], "mount --bind") {
		t.Errorf("no mount was run: %v", f.integrations[before:])
	}
	if got := ask(controlMount + "Debian"); !strings.HasPrefix(got, "error:") {
		t.Errorf("remount of a distro that is not integrated = %q", got)
	}
	if got := ask("something else"); !strings.HasPrefix(got, "error:") {
		t.Errorf("unknown request = %q", got)
	}
}

func TestAgentNoticesReachTheReceiver(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	got := make(chan string, 1)
	b.OnNotice(func(m string) { got <- m })
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := f.agents[0].Open([]byte(controlNotice + "C: is slow"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	select {
	case m := <-got:
		if m != "C: is slow" {
			t.Fatalf("notice = %q", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notice")
	}
}
