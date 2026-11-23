package engineapi

import (
	"encoding/json"
	"strconv"
	"strings"
)

// PortRequest is a host port a new container wants to publish.
type PortRequest struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"` // "tcp" or "udp"
}

// maxPortsPerRange bounds how many ports of a range like "8000-9000" are checked.
const maxPortsPerRange = 64

// PublishedPorts lists the fixed host ports in HostConfig.PortBindings of a
// container create body. Ports left to the engine (empty HostPort) are skipped.
func PublishedPorts(body []byte) []PortRequest {
	var doc struct {
		HostConfig *struct {
			PortBindings map[string][]struct{ HostIP, HostPort string }
		}
	}
	if json.Unmarshal(body, &doc) != nil || doc.HostConfig == nil {
		return nil
	}
	seen := map[PortRequest]bool{}
	var out []PortRequest
	for key, bindings := range doc.HostConfig.PortBindings {
		proto := "tcp"
		if _, p, ok := strings.Cut(key, "/"); ok && p != "" {
			proto = strings.ToLower(p)
		}
		for _, b := range bindings {
			for _, port := range expandPorts(b.HostPort) {
				r := PortRequest{Port: port, Proto: proto}
				if !seen[r] {
					seen[r] = true
					out = append(out, r)
				}
			}
		}
	}
	return out
}

func expandPorts(s string) []int {
	lo, hi, isRange := strings.Cut(strings.TrimSpace(s), "-")
	start, err := strconv.Atoi(lo)
	if err != nil || start <= 0 || start > 65535 {
		return nil
	}
	end := start
	if isRange {
		if end, err = strconv.Atoi(hi); err != nil || end < start || end > 65535 {
			return nil
		}
	}
	if end-start >= maxPortsPerRange {
		end = start + maxPortsPerRange - 1
	}
	var ports []int
	for p := start; p <= end; p++ {
		ports = append(ports, p)
	}
	return ports
}
