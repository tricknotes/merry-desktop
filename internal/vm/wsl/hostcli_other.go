//go:build !windows

package wsl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// There is no user PATH in the registry outside Windows.
func addToUserPath(string) error { return nil }

func runHostCommand(ctx context.Context, name string, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %v: %w: %s", name, args, err, errOut.String())
	}
	return out.String(), nil
}
