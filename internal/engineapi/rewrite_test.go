package engineapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWindowsPaths(t *testing.T) {
	tr := WindowsPaths("/mnt/wsl/engine/fs")
	cases := map[string]string{
		`C:\Users\me\proj`:            "/mnt/c/Users/me/proj",
		`d:/work/app`:                 "/mnt/d/work/app",
		`C:`:                          "/mnt/c",
		`\\wsl$\Ubuntu\home\me\src`:   "/mnt/wsl/engine/fs/Ubuntu/home/me/src",
		`\\wsl.localhost\Debian\srv`:  "/mnt/wsl/engine/fs/Debian/srv",
		`//wsl.localhost/Ubuntu/home`: "/mnt/wsl/engine/fs/Ubuntu/home",
	}
	for in, want := range cases {
		if got, ok := tr(in); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, unchanged := range []string{"/var/run/docker.sock", "data", `\\server\share\x`} {
		if got, ok := tr(unchanged); ok {
			t.Errorf("%q translated to %q, want unchanged", unchanged, got)
		}
	}
}

func TestSplitBind(t *testing.T) {
	cases := map[string][2]string{
		`C:\x:/y:ro`:   {`C:\x`, ":/y:ro"},
		`c:/x:/y`:      {`c:/x`, ":/y"},
		"/home/me:/y":  {"/home/me", ":/y"},
		"data:/var/db": {"data", ":/var/db"},
		"/only":        {"/only", ""},
		"v:/x":         {"v", ":/x"},
	}
	for in, want := range cases {
		src, rest := splitBind(in)
		if src != want[0] || rest != want[1] {
			t.Errorf("splitBind(%q) = %q, %q; want %q, %q", in, src, rest, want[0], want[1])
		}
	}
}

func TestRewriteContainerCreate(t *testing.T) {
	body := `{"Image":"nginx","Labels":{"com.docker.compose.project":"p"},"HostConfig":{
		"Binds":["C:\\proj\\src:/src:ro","data:/var/lib/data","/already/linux:/x"],
		"Mounts":[{"Type":"bind","Source":"C:\\proj\\conf","Target":"/etc/conf","ReadOnly":true},
		          {"Type":"volume","Source":"cache","Target":"/cache"}],
		"Memory":536870912}}`
	out, changed, err := RewriteContainerCreate([]byte(body), WindowsPaths(""))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	var doc struct {
		Image      string
		Labels     map[string]string
		HostConfig struct {
			Binds  []string
			Mounts []map[string]any
			Memory int64
		}
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	wantBinds := []string{"/mnt/c/proj/src:/src:ro", "data:/var/lib/data", "/already/linux:/x"}
	if !reflect.DeepEqual(doc.HostConfig.Binds, wantBinds) {
		t.Errorf("Binds = %v", doc.HostConfig.Binds)
	}
	if doc.HostConfig.Mounts[0]["Source"] != "/mnt/c/proj/conf" || doc.HostConfig.Mounts[0]["ReadOnly"] != true {
		t.Errorf("bind mount = %v", doc.HostConfig.Mounts[0])
	}
	if doc.HostConfig.Mounts[1]["Source"] != "cache" {
		t.Errorf("volume mount changed: %v", doc.HostConfig.Mounts[1])
	}
	if doc.Image != "nginx" || doc.Labels["com.docker.compose.project"] != "p" || doc.HostConfig.Memory != 536870912 {
		t.Errorf("other fields changed: %+v", doc)
	}
}

func TestRewriteLeavesUnrelatedBodiesAlone(t *testing.T) {
	for _, body := range []string{`{"Image":"x"}`, `{"Image":"x","HostConfig":null}`, `{"HostConfig":{"Binds":["v:/x"]}}`} {
		out, changed, err := RewriteContainerCreate([]byte(body), WindowsPaths(""))
		if err != nil || changed || string(out) != body {
			t.Errorf("%s -> %s, %v, %v", body, out, changed, err)
		}
	}
	if _, _, err := RewriteContainerCreate([]byte("not json"), WindowsPaths("")); err == nil {
		t.Error("want error for invalid JSON")
	}
}

func TestDistroPaths(t *testing.T) {
	tr := DistroPaths("/mnt/wsl/engine/distros/", "Ubuntu")
	cases := map[string]string{
		"/home/me/proj": "/mnt/wsl/engine/distros/Ubuntu/home/me/proj",
		"/":             "/mnt/wsl/engine/distros/Ubuntu",
		"/mnt/data/x":   "/mnt/wsl/engine/distros/Ubuntu/mnt/data/x",
	}
	for in, want := range cases {
		if got, ok := tr(in); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, unchanged := range []string{"/mnt/c/Users/me", "/mnt/d", "/mnt/wsl/shared/x", "volume"} {
		if got, ok := tr(unchanged); ok {
			t.Errorf("%q translated to %q", unchanged, got)
		}
	}
}

func TestInjectEnv(t *testing.T) {
	env := []string{"HTTP_PROXY=http://p:8080", "NO_PROXY=localhost"}
	out, changed, err := InjectEnv([]byte(`{"Image":"x","Env":["NO_PROXY=mine","A=1"]}`), env)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	var doc struct{ Env []string }
	_ = json.Unmarshal(out, &doc)
	want := []string{"NO_PROXY=mine", "A=1", "HTTP_PROXY=http://p:8080"}
	if !reflect.DeepEqual(doc.Env, want) {
		t.Errorf("Env = %v, want %v (the user's NO_PROXY wins)", doc.Env, want)
	}
	out, _, _ = InjectEnv([]byte(`{"Image":"x"}`), env)
	_ = json.Unmarshal(out, &doc)
	if len(doc.Env) != 2 {
		t.Errorf("Env without a list = %v", doc.Env)
	}
	if _, changed, _ := InjectEnv([]byte(`{"Image":"x"}`), nil); changed {
		t.Error("changed with no variables")
	}
}
