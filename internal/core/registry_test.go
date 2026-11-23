package core

import (
	"context"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

func TestNormalizeRegistry(t *testing.T) {
	ok := map[string]string{
		"registry.example.com":          "registry.example.com",
		"https://Registry.Example.com/": "registry.example.com",
		"harbor.corp:5000":              "harbor.corp:5000",
		"  http://10.0.0.5:8443  ":      "10.0.0.5:8443",
	}
	for in, want := range ok {
		if got, err := NormalizeRegistry(in); err != nil || got != want {
			t.Errorf("NormalizeRegistry(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "registry.example.com/path", "user@host", "host:", "has space", ".lead"} {
		if got, err := NormalizeRegistry(bad); err == nil {
			t.Errorf("NormalizeRegistry(%q) = %q, want error", bad, got)
		}
	}
}

type loginRecorder struct {
	*stub.Backend
	server, user, password string
}

func (l *loginRecorder) Login(_ context.Context, server, user, password string) error {
	l.server, l.user, l.password = server, user, password
	return nil
}

func TestLogin(t *testing.T) {
	l := &loginRecorder{Backend: newStub()}
	c := NewController(l, nil)
	if err := c.Login(context.Background(), "https://Harbor.corp/", " alice ", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if l.server != "harbor.corp" || l.user != "alice" || l.password != "s3cret" {
		t.Errorf("backend got %q %q %q", l.server, l.user, l.password)
	}
	if err := c.Login(context.Background(), "harbor.corp", "alice", ""); err == nil {
		t.Error("accepted an empty password")
	}
	if err := NewController(newStub(), nil).Login(context.Background(), "harbor.corp", "a", "b"); err == nil {
		t.Error("a backend without login support must refuse")
	}
}
