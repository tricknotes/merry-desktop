//go:build !windows

package wsl

// Outside Windows there is no WinINET proxy or Windows root store; only the
// environment variables are used. This keeps the package testable on Linux.
func hostProxy() (proxy, noProxy, note string) { return "", "", "" }

func hostRootCertificates() ([]byte, error) { return nil, nil }
