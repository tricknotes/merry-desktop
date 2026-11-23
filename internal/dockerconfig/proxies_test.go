package dockerconfig

import (
	"encoding/json"
	"testing"
)

var ours = Proxy{HTTPProxy: "http://proxy:3128", HTTPSProxy: "http://proxy:3128", NoProxy: "localhost"}

func TestApplyEnable(t *testing.T) {
	in := `{"auths":{"harbor.corp":{}},"credsStore":"wincred","currentContext":"x"}`
	out, res, err := Apply([]byte(in), ours, true)
	if err != nil || res != Written {
		t.Fatalf("res=%v err=%v", res, err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	if doc["credsStore"] != "wincred" || doc["currentContext"] != "x" || doc["auths"] == nil {
		t.Errorf("other keys changed: %s", out)
	}
	def := doc["proxies"].(map[string]any)["default"].(map[string]any)
	if def["httpProxy"] != "http://proxy:3128" || def["noProxy"] != "localhost" {
		t.Errorf("proxies.default = %v", def)
	}
	// Applying again changes nothing.
	if _, res, _ := Apply(out, ours, true); res != Unchanged {
		t.Errorf("second enable = %v", res)
	}
}

func TestApplyLeavesUsersOwnProxy(t *testing.T) {
	in := `{"proxies":{"default":{"httpProxy":"http://mine:8080"}}}`
	for _, enable := range []bool{true, false} {
		out, res, err := Apply([]byte(in), ours, enable)
		if err != nil || res != UsersOwn || string(out) != in {
			t.Errorf("enable=%v: res=%v out=%s err=%v", enable, res, out, err)
		}
	}
}

func TestApplyDisableRemovesOnlyOurs(t *testing.T) {
	enabled, _, _ := Apply([]byte(`{"proxies":{"other":{"httpProxy":"http://x"}}}`), ours, true)
	out, res, err := Apply(enabled, ours, false)
	if err != nil || res != Removed {
		t.Fatalf("res=%v err=%v", res, err)
	}
	var doc map[string]map[string]any
	_ = json.Unmarshal(out, &doc)
	if _, ok := doc["proxies"]["default"]; ok || doc["proxies"]["other"] == nil {
		t.Errorf("proxies = %v, want only other left", doc["proxies"])
	}
	// Removing the last entry removes the proxies key.
	only, _, _ := Apply([]byte(`{}`), ours, true)
	out, _, _ = Apply(only, ours, false)
	if string(out) != "{}\n" {
		t.Errorf("after removing the only entry: %q", out)
	}
	if _, res, _ := Apply([]byte(`{}`), ours, false); res != NotPresent {
		t.Errorf("disable without an entry = %v", res)
	}
}

func TestApplyEmptyOrInvalid(t *testing.T) {
	if _, res, err := Apply(nil, ours, true); err != nil || res != Written {
		t.Errorf("empty file: res=%v err=%v", res, err)
	}
	if _, _, err := Apply([]byte("{not json"), ours, true); err == nil {
		t.Error("want an error for invalid JSON")
	}
}
