package wsl

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestAddToPathValueKeepsTypeAndAvoidsDuplicates(t *testing.T) {
	const keyPath = `Software\merry-desktop-test\Environment`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	defer registry.DeleteKey(registry.CURRENT_USER, keyPath)
	defer registry.DeleteKey(registry.CURRENT_USER, `Software\merry-desktop-test`)

	k, _ = registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	_ = k.SetExpandStringValue("Path", `%USERPROFILE%\tools;C:\bin`)
	k.Close()

	dir := `C:\Users\test\AppData\Local\engine\bin`
	changed, err := addToPathValue(keyPath, dir)
	if err != nil || !changed {
		t.Fatalf("first add: changed=%v err=%v", changed, err)
	}
	changed, err = addToPathValue(keyPath, strings.ToLower(dir)+`\`)
	if err != nil || changed {
		t.Fatalf("second add: changed=%v err=%v (must not duplicate)", changed, err)
	}

	k, _ = registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	defer k.Close()
	got, typ, err := k.GetStringValue("Path")
	if err != nil {
		t.Fatal(err)
	}
	if typ != registry.EXPAND_SZ {
		t.Errorf("value type = %d, want REG_EXPAND_SZ", typ)
	}
	if want := `%USERPROFILE%\tools;C:\bin;` + dir; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestRunHostCommandPassesStdin(t *testing.T) {
	out, err := runHostCommand(context.Background(), []byte("b\r\na\r\n"), "sort.exe")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(out); strings.Join(got, ",") != "a,b" {
		t.Fatalf("sort output = %q", out)
	}
}
