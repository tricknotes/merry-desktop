package wsl

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestSetResourcesWritesWSLConfigWithBackup(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	ctx := context.Background()
	original := "# user settings\n[wsl2]\nswap=0\nmemory=2GB\n"
	if err := os.WriteFile(b.ConfigPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	cur, err := b.CurrentResources(ctx)
	if err != nil || cur.MemoryMiB != 2048 || cur.CPUs != 0 {
		t.Fatalf("CurrentResources = %+v, %v", cur, err)
	}
	want := vm.Resources{CPUs: 4, MemoryMiB: 8192}
	plan, err := b.PlanResources(ctx, want)
	if err != nil || !plan.RestartHost || !plan.RestartVM {
		t.Fatalf("PlanResources = %+v, %v; want host restart", plan, err)
	}
	if err := b.SetResources(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(b.ConfigPath)
	if string(got) != "# user settings\n[wsl2]\nswap=0\nmemory=8192MB\nprocessors=4\n" {
		t.Errorf(".wslconfig = %q", got)
	}
	backup, err := os.ReadFile(b.ConfigPath + ".bak-20261123-100000")
	if err != nil || string(backup) != original {
		t.Errorf("backup = %q, %v", backup, err)
	}
	if cur, _ := b.CurrentResources(ctx); cur != want {
		t.Errorf("CurrentResources after set = %+v", cur)
	}
	if plan, _ := b.PlanResources(ctx, want); plan.RestartHost || plan.RestartVM {
		t.Errorf("plan for unchanged values = %+v, want no restart", plan)
	}
}

func TestSetResourcesWithoutExistingFileMakesNoBackup(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{state: "Stopped"})
	if err := b.SetResources(context.Background(), vm.Resources{CPUs: 2, MemoryMiB: 4096}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(strings.TrimSuffix(b.ConfigPath, ".wslconfig"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak-") {
			t.Errorf("unexpected backup %s", e.Name())
		}
	}
}

func TestSetResourcesRejectsTooManyCPUs(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{state: "Stopped"})
	if err := b.SetResources(context.Background(), vm.Resources{CPUs: 9, MemoryMiB: 4096}); err == nil {
		t.Fatal("want error for more CPUs than the machine has")
	}
}

func TestDiskLimitIsAppliedOnNextStart(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	ctx := context.Background()
	r := vm.Resources{CPUs: 2, MemoryMiB: 4096, DiskGiB: 64}
	if err := b.SetResources(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Only the disk changes now: the engine VM restarts, the host does not.
	if plan, _ := b.PlanResources(ctx, vm.Resources{CPUs: 2, MemoryMiB: 4096, DiskGiB: 128}); plan.RestartHost || !plan.RestartVM {
		t.Errorf("plan for a disk change = %+v", plan)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.resized) != 1 || f.resized[0] != "64GB" {
		t.Fatalf("resized = %v, want [64GB]", f.resized)
	}
	// Restarting without a new request must not resize again.
	_ = b.Stop(ctx)
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.resized) != 1 {
		t.Errorf("resized again: %v", f.resized)
	}
}

func TestRestartHostShutsDownAndStartsAgain(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.RestartHost(ctx); err != nil {
		t.Fatal(err)
	}
	if f.shutdowns != 1 {
		t.Errorf("shutdowns = %d", f.shutdowns)
	}
	if len(f.started) != 2 {
		t.Errorf("keepalive sessions = %d, want 2 (started again)", len(f.started))
	}

	// When the engine was not running, it stays stopped.
	f2 := &fakeRunner{state: "Stopped"}
	b2 := newTestBackend(t, f2)
	if err := b2.RestartHost(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f2.started) != 0 {
		t.Errorf("started a stopped engine")
	}
}
