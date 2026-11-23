package engineapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ContainerCAPath is where new containers find the VM's CA bundle, which
// includes the host's extra certificates such as a corporate CA (F-11).
const ContainerCAPath = "/etc/ssl/merry-desktop/ca-certificates.crt"

// caEnv points the common tools at the bundle. A container that sets one of
// these keeps its own value.
var caEnv = []string{
	"SSL_CERT_FILE=" + ContainerCAPath,       // OpenSSL, Go, Python ssl, Ruby
	"REQUESTS_CA_BUNDLE=" + ContainerCAPath,  // Python requests, pip
	"CURL_CA_BUNDLE=" + ContainerCAPath,      // curl
	"NODE_EXTRA_CA_CERTS=" + ContainerCAPath, // Node.js, npm
	"GIT_SSL_CAINFO=" + ContainerCAPath,      // git
}

// InjectCA mounts bundle (a path in the VM) read-only at ContainerCAPath and
// sets caEnv, so that tools in the container trust what the host trusts. A
// container that already mounts something there is left alone.
func InjectCA(body []byte, bundle string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("container create body: %w", err)
	}
	var hostConfig map[string]json.RawMessage
	if raw, ok := doc["HostConfig"]; ok && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &hostConfig); err != nil {
			return nil, fmt.Errorf("HostConfig: %w", err)
		}
	}
	if hostConfig == nil {
		hostConfig = map[string]json.RawMessage{}
	}
	var mounts []map[string]any
	if raw, ok := hostConfig["Mounts"]; ok && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &mounts); err != nil {
			return nil, fmt.Errorf("Mounts: %w", err)
		}
	}
	for _, m := range mounts {
		if m["Target"] == ContainerCAPath {
			return body, nil
		}
	}
	var binds []string
	if raw, ok := hostConfig["Binds"]; ok && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &binds); err != nil {
			return nil, fmt.Errorf("Binds: %w", err)
		}
	}
	for _, b := range binds {
		_, rest := splitBind(b)
		if target, _, _ := strings.Cut(strings.TrimPrefix(rest, ":"), ":"); target == ContainerCAPath {
			return body, nil
		}
	}
	mounts = append(mounts, map[string]any{
		"Type": "bind", "Source": bundle, "Target": ContainerCAPath, "ReadOnly": true,
	})
	hostConfig["Mounts"] = mustMarshal(mounts)
	doc["HostConfig"] = mustMarshal(hostConfig)
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	out, _, err = InjectEnv(out, caEnv)
	return out, err
}
