package wsl

import "testing"

func TestProxyFromWinINET(t *testing.T) {
	cases := []struct {
		server, override, proxy, noProxy string
	}{
		{"proxy.example.com:8080", "", "http://proxy.example.com:8080", "localhost,127.0.0.1,::1"},
		{"http=p1.example.com:8080;https=p2.example.com:8443;ftp=f:21", "", "http://p2.example.com:8443", "localhost,127.0.0.1,::1"},
		{"http=p1.example.com:8080", "<local>", "http://p1.example.com:8080", "localhost,127.0.0.1,::1"},
		{"proxy.corp:3128", "<local>;*.corp.example;10.*;192.168.*.*;intranet", "http://proxy.corp:3128",
			"localhost,127.0.0.1,::1,.corp.example,10.0.0.0/8,192.168.0.0/16,intranet"},
		{"socks=s.example.com:1080", "", "", "localhost,127.0.0.1,::1"},
		{"https://secure.example.com:443", "10.*.5.*", "https://secure.example.com:443", "localhost,127.0.0.1,::1,10.*.5.*"},
	}
	for _, c := range cases {
		proxy, noProxy := proxyFromWinINET(c.server, c.override)
		if proxy != c.proxy || noProxy != c.noProxy {
			t.Errorf("(%q, %q) = (%q, %q); want (%q, %q)", c.server, c.override, proxy, noProxy, c.proxy, c.noProxy)
		}
	}
}

func TestScrubRemovesPassword(t *testing.T) {
	proxy := "http://alice:s3cr3t@proxy.example.com:8080"
	got := scrub("curl: failed via "+proxy+" (s3cr3t)", proxy)
	if want := "curl: failed via http://alice:xxxxx@proxy.example.com:8080 (xxxxx)"; got != want {
		t.Errorf("scrub = %q, want %q", got, want)
	}
	if redactProxy("http://proxy:8080") != "http://proxy:8080" {
		t.Error("URL without credentials must stay as is")
	}
}
