package wsl

import (
	"strings"
)

// distroState is the STATE column of `wsl --list --verbose`.
type distroState string

const (
	stateRunning    distroState = "Running"
	stateStopped    distroState = "Stopped"
	stateInstalling distroState = "Installing"
	stateConverting distroState = "Converting"
	stateUninstall  distroState = "Uninstalling"
)

type distro struct {
	Name    string
	State   distroState
	Version string
	Default bool
}

// parseList parses the output of `wsl --list --verbose`:
//
//	  NAME             STATE           VERSION
//	* Ubuntu           Running         2
//	  merry-engine     Stopped         2
//
// The header is skipped by position, so a localized header does not matter.
func parseList(out string) []distro {
	var distros []distro
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		d := distro{}
		if len(fields) > 0 && fields[0] == "*" {
			d.Default = true
			fields = fields[1:]
		}
		if len(fields) < 3 {
			continue
		}
		d.Name, d.State, d.Version = fields[0], distroState(fields[1]), fields[2]
		distros = append(distros, d)
	}
	return distros
}

func findDistro(distros []distro, name string) (distro, bool) {
	for _, d := range distros {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return distro{}, false
}
