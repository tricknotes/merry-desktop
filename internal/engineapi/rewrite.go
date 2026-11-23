// Package engineapi relays the Docker Engine API and rewrites host paths in
// bind mounts, so that existing Compose files and `docker run -v` keep working
// when the engine runs in a separate VM (REQUIREMENTS.md 6, compatibility).
package engineapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Translator maps a host path to the path the engine sees. ok=false leaves the
// path unchanged (for example a named volume, or a path already in the VM).
type Translator func(hostPath string) (enginePath string, ok bool)

var windowsDrivePath = regexp.MustCompile(`^([A-Za-z]):([\\/].*)?$`)

// wslSharePath matches \\wsl$\<distro>\... and \\wsl.localhost\<distro>\...
var wslSharePath = regexp.MustCompile(`(?i)^[\\/]{2}(wsl\$|wsl\.localhost)[\\/]([^\\/]+)(.*)$`)

// WindowsPaths translates paths from the Windows docker CLI:
//
//	C:\Users\me\proj  ->  /mnt/c/Users/me/proj   (WSL automount)
//	\\wsl$\Ubuntu\home\me -> <distroRoot>/Ubuntu/home/me
//
// distroRoot is where the root of other WSL distros is visible inside the
// engine VM; empty disables the second form.
func WindowsPaths(distroRoot string) Translator {
	return func(p string) (string, bool) {
		if m := windowsDrivePath.FindStringSubmatch(p); m != nil {
			rest := strings.ReplaceAll(m[2], `\`, "/")
			return "/mnt/" + strings.ToLower(m[1]) + rest, true
		}
		if m := wslSharePath.FindStringSubmatch(p); m != nil && distroRoot != "" {
			rest := strings.ReplaceAll(m[3], `\`, "/")
			return strings.TrimRight(distroRoot, "/") + "/" + m[2] + rest, true
		}
		return "", false
	}
}

// DistroPaths translates paths from a docker CLI inside another WSL distro.
// The distro's root is bind-mounted at <distroRoot>/<distro> in the engine VM:
//
//	/home/me/proj    -> <distroRoot>/<distro>/home/me/proj
//	/mnt/c/Users/me  -> unchanged (the engine VM has the same drive mounts)
//	/mnt/wsl/...     -> unchanged (shared by all WSL2 distros)
func DistroPaths(distroRoot, distro string) Translator {
	base := strings.TrimRight(distroRoot, "/") + "/" + distro
	return func(p string) (string, bool) {
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "/mnt/wsl/") || p == "/mnt/wsl" || driveMount.MatchString(p) {
			return "", false
		}
		if p == "/" {
			return base, true
		}
		return base + p, true
	}
}

// driveMount matches WSL's automount of Windows drives (/mnt/c, /mnt/c/...).
var driveMount = regexp.MustCompile(`^/mnt/[a-zA-Z](/|$)`)

// splitBind splits a HostConfig.Binds entry "src:dst[:opts]" where src may
// itself contain a drive letter colon (C:\x:/y:ro).
func splitBind(bind string) (src, rest string) {
	offset := 0
	// "C:\\x:/y" is a drive path, but "v:/x" is the named volume v. A drive path
	// always has a destination after it, so require another colon.
	if len(bind) >= 3 && bind[1] == ':' && isLetter(bind[0]) && (bind[2] == '\\' || bind[2] == '/') &&
		strings.Contains(bind[2:], ":") {
		offset = 2 // skip the drive letter colon
	}
	i := strings.Index(bind[offset:], ":")
	if i < 0 {
		return bind, ""
	}
	return bind[:offset+i], bind[offset+i:]
}

func isLetter(c byte) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

// isHostPath reports whether a bind source is a path (not a named volume).
func isHostPath(src string) bool {
	return strings.HasPrefix(src, "/") || strings.HasPrefix(src, `\`) || windowsDrivePath.MatchString(src)
}

// RewriteContainerCreate rewrites bind sources in the body of
// POST /containers/create: HostConfig.Binds and HostConfig.Mounts[].Source
// (Type "bind"). Everything else is kept as sent, including unknown fields.
func RewriteContainerCreate(body []byte, translate Translator) ([]byte, bool, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, fmt.Errorf("container create body: %w", err)
	}
	raw, ok := doc["HostConfig"]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return body, false, nil
	}
	var host map[string]json.RawMessage
	if err := json.Unmarshal(raw, &host); err != nil {
		return nil, false, fmt.Errorf("HostConfig: %w", err)
	}
	changed := false

	if rawBinds, ok := host["Binds"]; ok && !bytes.Equal(rawBinds, []byte("null")) {
		var binds []string
		if err := json.Unmarshal(rawBinds, &binds); err != nil {
			return nil, false, fmt.Errorf("HostConfig.Binds: %w", err)
		}
		for i, b := range binds {
			src, rest := splitBind(b)
			if !isHostPath(src) {
				continue
			}
			if to, ok := translate(src); ok {
				binds[i] = to + rest
				changed = true
			}
		}
		if changed {
			host["Binds"] = mustMarshal(binds)
		}
	}

	if rawMounts, ok := host["Mounts"]; ok && !bytes.Equal(rawMounts, []byte("null")) {
		var mounts []map[string]json.RawMessage
		if err := json.Unmarshal(rawMounts, &mounts); err != nil {
			return nil, false, fmt.Errorf("HostConfig.Mounts: %w", err)
		}
		mountsChanged := false
		for _, m := range mounts {
			var typ, src string
			_ = json.Unmarshal(m["Type"], &typ)
			_ = json.Unmarshal(m["Source"], &src)
			if typ != "bind" || !isHostPath(src) {
				continue
			}
			if to, ok := translate(src); ok {
				m["Source"] = mustMarshal(to)
				mountsChanged = true
			}
		}
		if mountsChanged {
			host["Mounts"] = mustMarshal(mounts)
			changed = true
		}
	}

	if !changed {
		return body, false, nil
	}
	doc["HostConfig"] = mustMarshal(host)
	out, err := json.Marshal(doc)
	return out, true, err
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only called with values decoded from JSON
	}
	return b
}
