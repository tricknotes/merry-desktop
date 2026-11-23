package engineapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestPublishedPorts(t *testing.T) {
	body := `{"HostConfig":{"PortBindings":{
		"80/tcp":[{"HostIp":"","HostPort":"8080"},{"HostIp":"::","HostPort":"8080"}],
		"53/udp":[{"HostPort":"5353"}],
		"9000/tcp":[{"HostPort":""}],
		"3000/tcp":[{"HostPort":"3000-3002"}],
		"1/tcp":[{"HostPort":"1-1000"}]}}}`
	got := PublishedPorts([]byte(body))
	sort.Slice(got, func(i, j int) bool { return got[i].Port < got[j].Port })
	var tcp, udp []int
	for _, p := range got {
		if p.Proto == "udp" {
			udp = append(udp, p.Port)
		} else {
			tcp = append(tcp, p.Port)
		}
	}
	if len(tcp) != 64+3+1 || tcp[0] != 1 || tcp[63] != 64 || tcp[64] != 3000 || tcp[67] != 8080 {
		t.Errorf("tcp = %v (a range is capped at 64 ports, duplicates removed)", tcp)
	}
	if len(udp) != 1 || udp[0] != 5353 {
		t.Errorf("udp = %v", udp)
	}
	if got := PublishedPorts([]byte(`{"Image":"x"}`)); got != nil {
		t.Errorf("no ports: %v", got)
	}
}

func TestProxyWarnsAboutTakenPorts(t *testing.T) {
	sock := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"abc","Warnings":[]}`))
	}))
	var asked []PortRequest
	var notified []string
	proxy := httptest.NewServer(NewProxyWith(sock, Options{
		CheckPorts: func(_ context.Context, ports []PortRequest) []string {
			asked = ports
			return []string{"port 8080 is taken"}
		},
		Notify: func(w string) { notified = append(notified, w) },
	}, nil))
	defer proxy.Close()
	resp, err := http.Post(proxy.URL+"/containers/create", "application/json",
		strings.NewReader(`{"HostConfig":{"PortBindings":{"80/tcp":[{"HostPort":"8080"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var created struct{ Warnings []string }
	_ = json.Unmarshal(b, &created)
	if len(asked) != 1 || asked[0].Port != 8080 || len(created.Warnings) != 1 || len(notified) != 1 {
		t.Fatalf("asked=%v warnings=%v notified=%v", asked, created.Warnings, notified)
	}
}
