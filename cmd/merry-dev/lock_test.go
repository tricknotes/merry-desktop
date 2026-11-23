//go:build linux

package main

import "testing"

func TestLockVMIsExclusive(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	unlock, err := lockVM("test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockVM("test"); err == nil {
		t.Fatal("second lock succeeded")
	}
	if u, err := lockVM("other"); err != nil {
		t.Fatalf("lock for another VM: %v", err)
	} else {
		u()
	}
	unlock()
	if u, err := lockVM("test"); err != nil {
		t.Fatalf("lock after unlock: %v", err)
	} else {
		u()
	}
}
