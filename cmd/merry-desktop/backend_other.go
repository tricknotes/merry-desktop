//go:build !windows

package main

import (
	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

// The macOS backend (Lima) comes in phase 3. Until then other OSes get the stub,
// which is enough to work on the UI.
func newBackend() vm.Backend {
	return stub.New()
}
