package main

import (
	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/wsl"
)

func newBackend() vm.Backend {
	return wsl.New(engineName())
}
