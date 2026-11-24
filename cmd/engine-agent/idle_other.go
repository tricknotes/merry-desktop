//go:build !linux

package main

import (
	"context"
	"errors"
)

// The agent runs only in the Linux VM; this lets the package build elsewhere.
func stopEngine(context.Context, string) error { return errors.New("not supported") }

func engineRunning(string) bool { return true }
