//go:build !windows

package wsl

// Outside Windows there is no host CLI to serve yet (tests use DialEngine).
func (b *Backend) startEndpoint() error { return nil }
