package wsl

import (
	"encoding/pem"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// hostProxy reads the WinINET proxy of the current user.
func hostProxy() (proxy, noProxy, note string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return "", "", ""
	}
	defer k.Close()
	if pac, _, err := k.GetStringValue("AutoConfigURL"); err == nil && pac != "" {
		note = "プロキシの自動構成スクリプト(PAC)が設定されていますが、評価できません。必要なら管理者に設定値を確認してください。"
	}
	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled != 1 {
		return "", "", note
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || server == "" {
		return "", "", note
	}
	override, _, _ := k.GetStringValue("ProxyOverride")
	proxy, noProxy = proxyFromWinINET(server, override)
	return proxy, noProxy, note
}

// hostRootCertificates returns the current user's view of the trusted root
// store (it includes the machine roots) as PEM.
func hostRootCertificates() ([]byte, error) {
	name, err := windows.UTF16PtrFromString("ROOT")
	if err != nil {
		return nil, err
	}
	store, err := windows.CertOpenSystemStore(0, name)
	if err != nil {
		return nil, fmt.Errorf("open root certificate store: %w", err)
	}
	defer windows.CertCloseStore(store, 0)

	var out []byte
	var ctx *windows.CertContext
	for {
		ctx, err = windows.CertEnumCertificatesInStore(store, ctx)
		if err != nil || ctx == nil {
			break
		}
		der := unsafe.Slice(ctx.EncodedCert, ctx.Length)
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: append([]byte(nil), der...)})...)
	}
	return out, nil
}
