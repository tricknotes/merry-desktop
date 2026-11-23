package wsl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDiagnosticsWhenStopped(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{state: "Stopped"})
	files, err := b.Diagnostics(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
		if strings.Contains(f.Name, "proxy.env") || strings.Contains(f.Name, "config.json") {
			t.Errorf("collected a sensitive file: %s", f.Name)
		}
	}
	for _, want := range []string{"host/wsl-list.txt", "host/wslconfig.txt", "vm/NOT-RUNNING.txt"} {
		if !names[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestSanitizeHosts(t *testing.T) {
	got := sanitizeHosts([]string{"harbor.corp:5000", "evil;rm -rf /", "", "$(id)", "registry-1.docker.io"})
	if want := []string{"harbor.corp:5000", "registry-1.docker.io"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeHosts = %v, want %v", got, want)
	}
}
