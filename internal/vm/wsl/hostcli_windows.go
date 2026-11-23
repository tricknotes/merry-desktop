package wsl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// addToUserPath appends dir to the user's PATH (HKCU\Environment) and tells
// running programs, so that new terminals see it. No administrator is needed.
func addToUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	current, typ, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	expand := func(s string) string {
		if v, err := registry.ExpandString(s); err == nil {
			return v
		}
		return s
	}
	updated, changed := withPathEntry(current, dir, expand)
	if !changed {
		return nil
	}
	if typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", updated)
	} else {
		err = k.SetStringValue("Path", updated)
	}
	if err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}

// broadcastEnvironmentChange sends WM_SETTINGCHANGE("Environment") like the
// System Properties dialog does, so Explorer passes the new PATH to new windows.
func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

func runHostCommand(ctx context.Context, name string, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %v: %w: %s", name, args, err, errOut.String())
	}
	return out.String(), nil
}
