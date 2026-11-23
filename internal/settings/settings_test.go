package settings

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStore(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "dir", "settings.json")}
	got, err := s.Load()
	if err != nil || got.Registries != nil {
		t.Fatalf("Load on a missing file = %+v, %v", got, err)
	}
	if _, err := s.Update(func(v *Settings) { v.Registries = append(v.Registries, "registry.example.com") }); err != nil {
		t.Fatal(err)
	}
	got, err = s.Load()
	if err != nil || !reflect.DeepEqual(got.Registries, []string{"registry.example.com"}) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}
