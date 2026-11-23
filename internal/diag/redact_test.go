package diag

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"proxy http://alice:s3cret@proxy.corp:8080 failed":      "proxy http://alice:xxxxx@proxy.corp:8080 failed",
		"Authorization: Bearer eyJhbGciOi.abc.def":              "Authorization: Bearer xxxxx",
		"Proxy-Authorization: Basic YWxpY2U6czNjcmV0":           "Proxy-Authorization: Basic xxxxx",
		`{"auth":"YWxpY2U6czNjcmV0","email":"a@b"}`:             `{"auth":"xxxxx","email":"a@b"}`,
		`{"identitytoken": "abc123"}`:                           `{"identitytoken": "xxxxx"}`,
		"password=hunter2 user=alice":                           "password=xxxxx user=alice",
		"DB_PASSWORD: 'p@ss'":                                   "DB_PASSWORD: 'xxxxx'",
		"GITHUB_TOKEN=ghp_abcdef;next":                          "GITHUB_TOKEN=xxxxx;next",
		"level=info msg=\"API listen on /var/run/docker.sock\"": "level=info msg=\"API listen on /var/run/docker.sock\"",
		"Server Version: 29.10.0":                               "Server Version: 29.10.0",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestRedactKnownValues(t *testing.T) {
	proxy := "http://proxy.corp:8080"
	got := Redact("HTTPS_PROXY="+proxy+" was used", proxy)
	if strings.Contains(got, "proxy.corp") {
		t.Errorf("known value not masked: %q", got)
	}
	if Redact("abc", "ab") != "abc" {
		t.Error("masked a value shorter than 4 characters")
	}
}
