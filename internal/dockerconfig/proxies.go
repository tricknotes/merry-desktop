// Package dockerconfig edits the "proxies" section of a docker CLI config file
// (~/.docker/config.json). The CLI passes these values to new containers and,
// unlike anything the engine side can do, to BuildKit builds as build args.
package dockerconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Proxy is the "proxies.default" entry.
type Proxy struct {
	HTTPProxy  string `json:"httpProxy,omitempty"`
	HTTPSProxy string `json:"httpsProxy,omitempty"`
	NoProxy    string `json:"noProxy,omitempty"`
}

// Result says what Apply did.
type Result string

const (
	Written    Result = "written"   // proxies.default was set to the given values
	Removed    Result = "removed"   // proxies.default (equal to ours) was removed
	Unchanged  Result = "unchanged" // nothing to do
	UsersOwn   Result = "users-own" // proxies.default holds other values; left alone
	NotPresent Result = "not-present"
)

// Apply sets (enable) or removes (!enable) proxies.default in a config file's
// content, keeping every other key as it was. Values someone else wrote are
// never changed: enabling leaves a different proxies.default alone, and
// disabling only removes an entry equal to p.
func Apply(content []byte, p Proxy, enable bool) ([]byte, Result, error) {
	doc := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(content)) > 0 {
		if err := json.Unmarshal(content, &doc); err != nil {
			return nil, "", fmt.Errorf("config.json: %w", err)
		}
	}
	proxies := map[string]json.RawMessage{}
	if raw, ok := doc["proxies"]; ok && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &proxies); err != nil {
			return nil, "", fmt.Errorf("config.json proxies: %w", err)
		}
	}
	var current *Proxy
	if raw, ok := proxies["default"]; ok {
		current = &Proxy{}
		if err := json.Unmarshal(raw, current); err != nil {
			return nil, "", fmt.Errorf("config.json proxies.default: %w", err)
		}
	}

	switch {
	case enable && current != nil && *current == p:
		return content, Unchanged, nil
	case enable && current != nil:
		return content, UsersOwn, nil
	case enable:
		b, _ := json.Marshal(p)
		proxies["default"] = b
	case current == nil:
		return content, NotPresent, nil
	case *current != p:
		return content, UsersOwn, nil
	default:
		delete(proxies, "default")
	}

	if len(proxies) == 0 {
		delete(doc, "proxies")
	} else {
		b, _ := json.Marshal(proxies)
		doc["proxies"] = b
	}
	out, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		return nil, "", err
	}
	out = append(out, '\n')
	if enable {
		return out, Written, nil
	}
	return out, Removed, nil
}
