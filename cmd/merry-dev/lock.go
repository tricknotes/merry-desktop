//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockVM takes an exclusive lock for the VM, so that only one merry-dev
// process holds its agent session. The lock is released when the process
// exits, even on a crash.
func lockVM(name string) (func(), error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "merry-dev-"+name+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s はほかの merry-dev(start / ui / setup)が使っています。そちらを終えてから実行してください", name)
	}
	return func() { _ = f.Close() }, nil
}
