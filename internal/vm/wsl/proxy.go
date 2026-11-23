package wsl

import (
	"fmt"
	"net/url"
	"strings"
)

// proxyFromWinINET converts the user's WinINET proxy settings (Settings >
// Network > Proxy) to a proxy URL and a NO_PROXY list:
//
//	ProxyServer:   "host:port" or "http=host:port;https=host:port;..."
//	ProxyOverride: "<local>;*.example.com;10.*"
//
// It returns "" when no HTTP(S) proxy is configured (a SOCKS-only setting,
// for example). The same conversion as scripts/phase0/wsl2/host-network.ps1.
func proxyFromWinINET(server, override string) (proxy, noProxy string) {
	server = strings.TrimSpace(server)
	if strings.Contains(server, "=") {
		m := map[string]string{}
		for _, part := range strings.Split(server, ";") {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
		}
		server = m["https"]
		if server == "" {
			server = m["http"]
		}
	}
	if server != "" {
		if strings.Contains(server, "://") {
			proxy = server
		} else {
			proxy = "http://" + server
		}
	}

	entries := []string{"localhost", "127.0.0.1", "::1"}
	for _, e := range strings.Split(override, ";") {
		e = strings.TrimSpace(e)
		switch {
		case e == "" || e == "<local>":
			// <local> means plain host names; NO_PROXY cannot express it.
		case strings.HasPrefix(e, "*."):
			entries = append(entries, e[1:])
		default:
			if cidr, ok := wildcardToCIDR(e); ok {
				entries = append(entries, cidr)
			} else {
				entries = append(entries, e)
			}
		}
	}
	return proxy, strings.Join(unique(entries), ",")
}

// wildcardToCIDR turns "10.*" or "192.168.*.*" into a CIDR block.
func wildcardToCIDR(s string) (string, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 || parts[len(parts)-1] != "*" {
		return "", false
	}
	var fixed []string
	for i, p := range parts {
		if p == "*" {
			for _, rest := range parts[i:] {
				if rest != "*" {
					return "", false
				}
			}
			break
		}
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return "", false
		}
		fixed = append(fixed, p)
	}
	if len(fixed) == 0 {
		return "", false
	}
	bits := 8 * len(fixed)
	for len(fixed) < 4 {
		fixed = append(fixed, "0")
	}
	return fmt.Sprintf("%s/%d", strings.Join(fixed, "."), bits), true
}

func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// redactProxy removes the password from a proxy URL, for logs and the UI.
func redactProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		if err != nil {
			return "(invalid proxy URL)"
		}
		return raw
	}
	return u.Redacted()
}

// scrub removes a proxy URL (and its password) from text shown to the user.
func scrub(text, proxy string) string {
	if proxy == "" {
		return text
	}
	text = strings.ReplaceAll(text, proxy, redactProxy(proxy))
	if u, err := url.Parse(proxy); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			text = strings.ReplaceAll(text, pw, "xxxxx")
		}
	}
	return text
}
