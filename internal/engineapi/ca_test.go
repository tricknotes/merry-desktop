package engineapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectCA(t *testing.T) {
	body := `{"Image":"node","Env":["NODE_EXTRA_CA_CERTS=/mine.pem"],"HostConfig":{"Binds":["/src:/app"]}}`
	out, err := InjectCA([]byte(body), "/etc/ssl/certs/ca-certificates.crt")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Env        []string
		HostConfig struct {
			Binds  []string
			Mounts []struct {
				Type, Source, Target string
				ReadOnly             bool
			}
		}
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	m := doc.HostConfig.Mounts
	if len(m) != 1 || m[0].Type != "bind" || m[0].Source != "/etc/ssl/certs/ca-certificates.crt" ||
		m[0].Target != ContainerCAPath || !m[0].ReadOnly || len(doc.HostConfig.Binds) != 1 {
		t.Errorf("host config = %+v", doc.HostConfig)
	}
	env := strings.Join(doc.Env, " ")
	if !strings.Contains(env, "SSL_CERT_FILE="+ContainerCAPath) || !strings.Contains(env, "NODE_EXTRA_CA_CERTS=/mine.pem") ||
		strings.Contains(env, "NODE_EXTRA_CA_CERTS="+ContainerCAPath) {
		t.Errorf("env = %v (the container's own value must stay)", doc.Env)
	}

	// No HostConfig at all.
	if out, err := InjectCA([]byte(`{"Image":"x"}`), "/b"); err != nil || !strings.Contains(string(out), ContainerCAPath) {
		t.Errorf("without HostConfig: %s, %v", out, err)
	}
	// Something is mounted there already: unchanged.
	own := `{"HostConfig":{"Binds":["/my.crt:` + ContainerCAPath + `:ro"]}}`
	if out, _ := InjectCA([]byte(own), "/b"); string(out) != own {
		t.Errorf("own mount changed: %s", out)
	}
}
