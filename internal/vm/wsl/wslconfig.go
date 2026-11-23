package wsl

import (
	"fmt"
	"strconv"
	"strings"
)

// wslConfig is the part of %UserProfile%\.wslconfig that the app reads and writes.
// Zero means "not set" (WSL then uses its own default).
type wslConfig struct {
	Processors int
	MemoryMiB  int
}

const wsl2Section = "wsl2"

// parseWSLConfig reads processors and memory from the [wsl2] section.
// Unknown keys, other sections and comments are ignored.
func parseWSLConfig(content string) (wslConfig, error) {
	var cfg wslConfig
	section := ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if name, ok := sectionName(line); ok {
			section = name
			continue
		}
		if section != wsl2Section {
			continue
		}
		key, value, ok := keyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "processors":
			n, err := strconv.Atoi(value)
			if err != nil {
				return cfg, fmt.Errorf(".wslconfig: processors=%q: %w", value, err)
			}
			cfg.Processors = n
		case "memory":
			mib, err := parseSize(value)
			if err != nil {
				return cfg, fmt.Errorf(".wslconfig: memory=%q: %w", value, err)
			}
			cfg.MemoryMiB = mib
		}
	}
	return cfg, nil
}

// updateWSLConfig sets processors and memory in the [wsl2] section and keeps
// everything else (other keys, sections, comments, line endings) as it was.
// A zero value removes the key, so WSL uses its default. The section is
// appended if it does not exist.
func updateWSLConfig(content string, cfg wslConfig) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	values := map[string]string{"processors": "", "memory": ""}
	if cfg.Processors > 0 {
		values["processors"] = strconv.Itoa(cfg.Processors)
	}
	if cfg.MemoryMiB > 0 {
		values["memory"] = fmt.Sprintf("%dMB", cfg.MemoryMiB)
	}
	written := map[string]bool{}

	var out []string
	section := ""
	sectionEnd := -1 // index in out after the last line of [wsl2]
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if name, ok := sectionName(line); ok {
			section = name
			out = append(out, raw)
			if section == wsl2Section {
				sectionEnd = len(out)
			}
			continue
		}
		if section == wsl2Section {
			if key, _, ok := keyValue(line); ok {
				if v, managed := values[key]; managed {
					if written[key] || v == "" {
						written[key] = true
						continue // drop duplicates, and keys reset to the default
					}
					out = append(out, key+"="+v)
					written[key] = true
					sectionEnd = len(out)
					continue
				}
			}
			if line != "" {
				sectionEnd = len(out) + 1
			}
		}
		out = append(out, raw)
	}

	var missing []string
	for _, key := range []string{"processors", "memory"} {
		if !written[key] && values[key] != "" {
			missing = append(missing, key+"="+values[key])
		}
	}
	if len(missing) > 0 {
		if sectionEnd < 0 {
			// No [wsl2] yet: append it, separated by a blank line.
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, "["+wsl2Section+"]")
			out = append(out, missing...)
			out = append(out, "")
		} else {
			tail := append([]string{}, out[sectionEnd:]...)
			out = append(append(out[:sectionEnd], missing...), tail...)
		}
	}
	return strings.Join(out, newline)
}

func sectionName(line string) (string, bool) {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.ToLower(strings.TrimSpace(line[1 : len(line)-1])), true
	}
	return "", false
}

func keyValue(line string) (key, value string, ok bool) {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return "", "", false
	}
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	v = strings.TrimSpace(v)
	// Drop an inline comment.
	if i := strings.IndexAny(v, "#;"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return strings.ToLower(strings.TrimSpace(k)), v, true
}

// parseSize converts a .wslconfig size ("8GB", "4096MB", "1024KB", bytes) to MiB.
func parseSize(s string) (int, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	units := []struct {
		suffix string
		mib    float64
	}{
		{"TB", 1024 * 1024}, {"GB", 1024}, {"MB", 1}, {"KB", 1.0 / 1024},
		{"T", 1024 * 1024}, {"G", 1024}, {"M", 1}, {"K", 1.0 / 1024},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), 64)
			if err != nil {
				return 0, err
			}
			return int(n * u.mib), nil
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int(n / 1024 / 1024), nil
}
