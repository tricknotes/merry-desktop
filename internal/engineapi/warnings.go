package engineapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// warningsHeader carries warnings from the request side of the proxy to the
// response side. dockerd ignores it.
const warningsHeader = "X-Merry-Warnings"

// SlowBindWarnings returns a warning for every bind mount whose source ends up
// on a Windows drive inside the VM (/mnt/c/...). Such mounts go through the 9p
// file share and are much slower than files inside a WSL distro, which is the
// most common cause of slow builds and file watchers (I-09).
func SlowBindWarnings(body []byte, translate Translator) []string {
	var doc struct {
		HostConfig *struct {
			Binds  []string
			Mounts []struct{ Type, Source string }
		}
	}
	if json.Unmarshal(body, &doc) != nil || doc.HostConfig == nil {
		return nil
	}
	var sources []string
	for _, b := range doc.HostConfig.Binds {
		if src, _ := splitBind(b); isHostPath(src) {
			sources = append(sources, src)
		}
	}
	for _, m := range doc.HostConfig.Mounts {
		if m.Type == "bind" && isHostPath(m.Source) {
			sources = append(sources, m.Source)
		}
	}
	var warnings []string
	seen := map[string]bool{}
	for _, src := range sources {
		final := src
		if translate != nil {
			if to, ok := translate(src); ok {
				final = to
			}
		}
		if !driveMount.MatchString(final) || seen[src] {
			continue
		}
		seen[src] = true
		warnings = append(warnings, fmt.Sprintf(
			"%s は Windows のドライブ上にあります。コンテナからの読み書きは、WSL のディストリ上のファイルより大幅に遅くなります。"+
				"速くするには、WSL のディストリ(例: \\\\wsl$\\Ubuntu\\home\\...)にファイルを置いてください。", src))
	}
	return warnings
}

func encodeWarnings(w []string) string {
	b, _ := json.Marshal(w)
	return base64.StdEncoding.EncodeToString(b)
}

func decodeWarnings(s string) []string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var w []string
	_ = json.Unmarshal(b, &w)
	return w
}

// addResponseWarnings appends the request's warnings to the "Warnings" list
// of a container create response, which the docker CLI prints as WARNING lines.
func addResponseWarnings(resp *http.Response) error {
	if resp.Request == nil || resp.StatusCode != http.StatusCreated {
		return nil
	}
	extra := decodeWarnings(resp.Request.Header.Get(warningsHeader))
	if len(extra) == 0 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(body, &doc) != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	var current []string
	if raw, ok := doc["Warnings"]; ok {
		_ = json.Unmarshal(raw, &current)
	}
	doc["Warnings"] = mustMarshal(append(current, extra...))
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
	resp.Header.Del("Transfer-Encoding")
	return nil
}
