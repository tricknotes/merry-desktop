package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

// provisioning is a stub VM that also implements vm.Provisioner.
type provisioning struct {
	*stub.Backend
	hostErr     error
	provisioned bool
	provisions  int
	destroyed   int
	helloOutput string
}

func (p *provisioning) CheckHost(context.Context) error { return p.hostErr }
func (p *provisioning) DetectNetwork(context.Context) (vm.Network, error) {
	return vm.Network{ProxyURL: "http://u:pw@proxy:8080", CACertsPEM: []byte("-----BEGIN CERTIFICATE-----\n")}, nil
}
func (p *provisioning) Provisioned(context.Context) (bool, error) { return p.provisioned, nil }
func (p *provisioning) Provision(_ context.Context, _ vm.Network, progress func(string)) error {
	p.provisions++
	progress("downloading")
	p.provisioned = true
	return nil
}
func (p *provisioning) Destroy(ctx context.Context) error {
	p.destroyed++
	p.provisioned = false
	return p.Backend.Stop(ctx)
}

func (p *provisioning) Exec(ctx context.Context, args []string) (vm.ExecResult, error) {
	if _, err := p.Backend.Exec(ctx, args); err != nil {
		return vm.ExecResult{}, err
	}
	return vm.ExecResult{Stdout: p.helloOutput}, nil
}

func newProvisioning() *provisioning {
	return &provisioning{Backend: newStub(), helloOutput: "\nHello from Docker!\n"}
}

func runSetup(t *testing.T, p *provisioning) ([]SetupEvent, error) {
	t.Helper()
	c := NewController(p, nil)
	var events []SetupEvent
	err := c.Setup(context.Background(), func(e SetupEvent) { events = append(events, e) })
	return events, err
}

func finalStates(events []SetupEvent) map[vm.SetupStep]SetupState {
	m := map[vm.SetupStep]SetupState{}
	for _, e := range events {
		m[e.Step] = e.State
	}
	return m
}

func TestSetupRunsAllSteps(t *testing.T) {
	p := newProvisioning()
	events, err := runSetup(t, p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[vm.SetupStep]SetupState{
		vm.StepCheckHost: SetupDone, vm.StepNetwork: SetupDone, vm.StepCreate: SetupDone,
		vm.StepStart: SetupDone, vm.StepVerify: SetupDone,
	}
	if got := finalStates(events); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for _, e := range events {
		if strings.Contains(e.Message, "pw") || strings.Contains(e.Message, "proxy:8080") {
			t.Errorf("event shows the proxy: %+v", e)
		}
	}
	// Running again skips creating the VM.
	events, err = runSetup(t, p)
	if err != nil || finalStates(events)[vm.StepCreate] != SetupSkipped || p.provisions != 1 {
		t.Fatalf("second run: err=%v states=%v provisions=%d", err, finalStates(events), p.provisions)
	}
}

func TestSetupStopsWhenHostIsNotReady(t *testing.T) {
	p := newProvisioning()
	p.hostErr = fmt.Errorf("%w: 管理者に依頼してください", vm.ErrHostNotReady)
	events, err := runSetup(t, p)
	if !errors.Is(err, vm.ErrHostNotReady) {
		t.Fatalf("err = %v", err)
	}
	last := events[len(events)-1]
	if last.Step != vm.StepCheckHost || last.State != SetupFailed || last.Message != "管理者に依頼してください" {
		t.Fatalf("last event = %+v", last)
	}
	if p.provisions != 0 {
		t.Error("provisioned on a host that is not ready")
	}
}

func TestSetupFailsWhenHelloWorldFails(t *testing.T) {
	p := newProvisioning()
	p.helloOutput = "Unable to find image 'hello-world:latest' locally\nerror pulling image"
	events, err := runSetup(t, p)
	if err == nil || finalStates(events)[vm.StepVerify] != SetupFailed {
		t.Fatalf("err=%v states=%v", err, finalStates(events))
	}
}

func TestNeedsSetup(t *testing.T) {
	p := newProvisioning()
	c := NewController(p, nil)
	if need, _ := c.NeedsSetup(context.Background()); !need {
		t.Error("NeedsSetup = false before provisioning")
	}
	p.provisioned = true
	if need, _ := c.NeedsSetup(context.Background()); need {
		t.Error("NeedsSetup = true after provisioning")
	}
	if need, _ := NewController(newStub(), nil).NeedsSetup(context.Background()); need {
		t.Error("a backend without provisioning never needs setup")
	}
}

func TestRecreateDestroysAndSetsUpAgain(t *testing.T) {
	p := newProvisioning()
	c := NewController(p, nil)
	ctx := context.Background()
	if err := c.Setup(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var events []SetupEvent
	if err := c.Recreate(ctx, func(e SetupEvent) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	if p.destroyed != 1 || p.provisions != 2 {
		t.Fatalf("destroyed=%d provisions=%d, want 1 and 2", p.destroyed, p.provisions)
	}
	if events[0].Step != vm.StepCreate || events[0].Message != "いまの VM を削除しています" {
		t.Errorf("first event = %+v", events[0])
	}
	if got := finalStates(events)[vm.StepVerify]; got != SetupDone {
		t.Errorf("verify = %q after recreate", got)
	}
	if s := c.Snapshot(); s.Status != vm.StatusRunning {
		t.Errorf("status after recreate = %q", s.Status)
	}
}
