package wsl

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/engineapi"
)

const netstatTCP = `
アクティブな接続

  プロトコル  ローカル アドレス      外部アドレス           状態            PID
  TCP         0.0.0.0:135            0.0.0.0:0              LISTENING       2024
  TCP         127.0.0.1:8080         0.0.0.0:0              LISTENING       5120
  TCP         192.168.1.5:50000      20.1.1.1:443           ESTABLISHED     7000
  TCP         [::]:5432              [::]:0                 LISTENING       880
`

func TestParseNetstat(t *testing.T) {
	got := parseNetstat(netstatTCP, "tcp")
	want := []hostListener{{"tcp", 135, 2024, ""}, {"tcp", 8080, 5120, ""}, {"tcp", 5432, 880, ""}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	udp := parseNetstat("  UDP    0.0.0.0:5353           *:*                                    3260\n", "udp")
	if len(udp) != 1 || udp[0].Port != 5353 || udp[0].PID != 3260 {
		t.Errorf("udp = %+v", udp)
	}
}

func TestPortWarnings(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{})
	b.HostPorts = func(context.Context) ([]hostListener, error) {
		return []hostListener{{"tcp", 8080, 5120, "node.exe"}, {"tcp", 5432, 880, "wslrelay.exe"}}, nil
	}
	got := b.portWarnings(context.Background(), []engineapi.PortRequest{{Port: 8080, Proto: "tcp"}, {Port: 8080, Proto: "udp"}, {Port: 5432, Proto: "tcp"}, {Port: 3000, Proto: "tcp"}})
	if len(got) != 2 || !strings.Contains(got[0], "node.exe、PID 5120") || !strings.Contains(got[1], "WSL の中のほかのサービス") {
		t.Fatalf("warnings = %q", got)
	}
}

func TestAgentCanAskAboutPorts(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	b.HostPorts = func(context.Context) ([]hostListener, error) {
		return []hostListener{{"tcp", 8080, 1, "node.exe"}}, nil
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal([]engineapi.PortRequest{{Port: 8080, Proto: "tcp"}, {Port: 3000, Proto: "tcp"}})
	st, err := f.agents[0].Open(append([]byte(controlPorts), req...))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	body, _ := io.ReadAll(st)
	var warnings []string
	if err := json.Unmarshal(body, &warnings); err != nil || len(warnings) != 1 {
		t.Fatalf("reply %s, %v", body, err)
	}
}
