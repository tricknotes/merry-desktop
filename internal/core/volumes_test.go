package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

// volumeVM answers the volume scripts and maps host paths under /host.
type volumeVM struct {
	*stub.Backend
	args []string
	res  vm.ExecResult
}

func (v *volumeVM) Exec(_ context.Context, args []string) (vm.ExecResult, error) {
	v.args = args
	return v.res, nil
}

func (v *volumeVM) VMPath(p string) (string, error) { return "/mnt/c" + p, nil }

func runningVolumeVM(t *testing.T, res vm.ExecResult) (*Controller, *volumeVM) {
	b := &volumeVM{Backend: newStub(), res: res}
	c := NewController(b, nil)
	c.Start(context.Background())
	return c, b
}

func TestBackupFileNames(t *testing.T) {
	name := BackupFileName("proj_db-data", time.Date(2026, 11, 24, 9, 5, 7, 0, time.UTC))
	if name != "proj_db-data-20261124-090507.tar.gz" || !IsBackupFile(name) {
		t.Fatalf("name = %q", name)
	}
	if v := VolumeFromBackup(name); v != "proj_db-data" {
		t.Fatalf("volume = %q", v)
	}
	if v := VolumeFromBackup("copied.tar.gz"); v != "copied" {
		t.Fatalf("volume = %q", v)
	}
	for _, bad := range []string{"../x.tar.gz", "x.tar", ".hidden.tar.gz", "a b.tar.gz"} {
		if IsBackupFile(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBackupVolume(t *testing.T) {
	c, b := runningVolumeVM(t, vm.ExecResult{Stdout: "in-use\tproj-db-1\nsize\t2048\n"})
	r, err := c.BackupVolume(context.Background(), "proj_data", "/backups")
	if err != nil {
		t.Fatal(err)
	}
	if r.SizeBytes != 2048 || len(r.InUseBy) != 1 || r.InUseBy[0] != "proj-db-1" || !strings.HasPrefix(r.File, "proj_data-") {
		t.Fatalf("result = %+v", r)
	}
	// The values go as arguments, never into the script.
	if got := b.args[len(b.args)-3:]; got[0] != "proj_data" || got[1] != "/mnt/c/backups" || got[2] != r.File {
		t.Fatalf("args = %v", got)
	}
}

func TestBackupVolumeRejectsBadNames(t *testing.T) {
	c, b := runningVolumeVM(t, vm.ExecResult{})
	for _, name := range []string{"", "-x", "a;rm", "a/b", "$(x)"} {
		if _, err := c.BackupVolume(context.Background(), name, "/b"); err == nil {
			t.Errorf("%q accepted", name)
		}
		if err := c.RestoreVolume(context.Background(), "/b/x-20261124-090507.tar.gz", name); err == nil {
			t.Errorf("restore into %q accepted", name)
		}
	}
	if b.args != nil {
		t.Fatalf("ran %v", b.args)
	}
}

func TestRestoreVolumeShowsTheScriptsMessage(t *testing.T) {
	c, _ := runningVolumeVM(t, vm.ExecResult{ExitCode: 1, Stderr: "error: ボリューム v は空ではありません\n"})
	err := c.RestoreVolume(context.Background(), "/b/v-20261124-090507.tar.gz", "v")
	if err == nil || !strings.Contains(err.Error(), "空ではありません") {
		t.Fatalf("err = %v", err)
	}
	if err := c.RestoreVolume(context.Background(), "/b/notes.txt", "v"); err == nil {
		t.Fatal("restored a file that is not a backup")
	}
}

func TestVolumes(t *testing.T) {
	c, _ := runningVolumeVM(t, vm.ExecResult{Stdout: "proj_data\tproj\nloose\t\n"})
	v, err := c.Volumes(context.Background())
	if err != nil || len(v) != 2 || v[0] != (VolumeInfo{Name: "proj_data", Project: "proj"}) || v[1].Project != "" {
		t.Fatalf("volumes = %+v, %v", v, err)
	}
}
