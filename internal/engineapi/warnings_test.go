package engineapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlowBindWarnings(t *testing.T) {
	body := `{"HostConfig":{"Binds":["C:\\proj:/src","C:\\proj:/again","data:/var/db","\\\\wsl$\\Ubuntu\\home\\me:/fast"],
		"Mounts":[{"Type":"bind","Source":"D:\\cache","Target":"/cache"},{"Type":"volume","Source":"v","Target":"/v"}]}}`
	got := SlowBindWarnings([]byte(body), WindowsPaths("/mnt/wsl/e/distros"))
	if len(got) != 2 || !strings.HasPrefix(got[0], `C:\proj は`) || !strings.HasPrefix(got[1], `D:\cache は`) {
		t.Fatalf("windows client warnings = %q", got)
	}
	// From a distro: /mnt/c is slow, its own home is not.
	got = SlowBindWarnings([]byte(`{"HostConfig":{"Binds":["/mnt/c/Users/me/proj:/src","/home/me/proj:/fast"]}}`), DistroPaths("/mnt/wsl/e/distros", "Ubuntu"))
	if len(got) != 1 || !strings.HasPrefix(got[0], "/mnt/c/Users/me/proj は") {
		t.Fatalf("distro client warnings = %q", got)
	}
	if got := SlowBindWarnings([]byte(`{"Image":"x"}`), nil); got != nil {
		t.Errorf("no binds: %q", got)
	}
}

func TestProxyAddsWarningsToTheCreateResponse(t *testing.T) {
	sock := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"abc","Warnings":["from engine"]}`))
	}))
	proxy := httptest.NewServer(NewProxyWith(sock, Options{Translate: WindowsPaths(""), WarnSlowBinds: true}, nil))
	defer proxy.Close()

	resp, err := http.Post(proxy.URL+"/v1.57/containers/create", "application/json",
		strings.NewReader(`{"HostConfig":{"Binds":["C:\\proj:/src"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created struct {
		Id       string
		Warnings []string
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if created.Id != "abc" || len(created.Warnings) != 2 || created.Warnings[0] != "from engine" || !strings.Contains(created.Warnings[1], `C:\proj`) {
		t.Fatalf("response = %+v", created)
	}

	// A create without slow binds is passed through unchanged.
	resp2, _ := http.Post(proxy.URL+"/v1.57/containers/create", "application/json", strings.NewReader(`{"Image":"x"}`))
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(b2) != `{"Id":"abc","Warnings":["from engine"]}` {
		t.Errorf("unchanged response = %s", b2)
	}
}
