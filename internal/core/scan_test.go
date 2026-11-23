package core

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

const trivyOutput = `{"SchemaVersion":2,"Results":[
 {"Target":"app (alpine 3.20.0)","Class":"os-pkgs","Vulnerabilities":[
  {"VulnerabilityID":"CVE-1","PkgName":"openssl","InstalledVersion":"3.0","FixedVersion":"3.1","Severity":"MEDIUM","Title":"m"},
  {"VulnerabilityID":"CVE-2","PkgName":"zlib","InstalledVersion":"1.2","FixedVersion":"","Severity":"CRITICAL","Title":"c"}]},
 {"Target":"app/package-lock.json","Class":"lang-pkgs","Vulnerabilities":[
  {"VulnerabilityID":"GHSA-3","PkgName":"lodash","InstalledVersion":"4.0","FixedVersion":"4.17","Severity":"HIGH","Title":"h"}]},
 {"Target":"empty"}]}`

func TestParseTrivy(t *testing.T) {
	r, err := parseTrivy("app:1", trivyOutput)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["CRITICAL"] != 1 || r.Counts["HIGH"] != 1 || r.Counts["MEDIUM"] != 1 || r.Counts["LOW"] != 0 {
		t.Errorf("counts = %v", r.Counts)
	}
	var ids []string
	for _, v := range r.Vulnerabilities {
		ids = append(ids, v.ID)
	}
	if !slices.Equal(ids, []string{"CVE-2", "GHSA-3", "CVE-1"}) || r.Vulnerabilities[1].Target != "app/package-lock.json" {
		t.Errorf("vulnerabilities = %+v", r.Vulnerabilities)
	}
	if _, err := parseTrivy("x", "not json"); err == nil {
		t.Error("bad output parsed")
	}
}

type scanBackend struct {
	*stub.Backend
	args [][]string
	out  string
}

func (b *scanBackend) Exec(_ context.Context, args []string) (vm.ExecResult, error) {
	b.args = append(b.args, args)
	return vm.ExecResult{Stdout: b.out}, nil
}

func TestScanImage(t *testing.T) {
	b := &scanBackend{Backend: stub.New(), out: trivyOutput}
	c := NewController(b, nil)
	ctx := context.Background()
	if _, err := c.ScanImage(ctx, "app:1"); err == nil {
		t.Error("scan while stopped")
	}
	c.Start(ctx)
	for _, bad := range []string{"", "--help", "a b"} {
		if _, err := c.ScanImage(ctx, bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	r, err := c.ScanImage(ctx, " app:1 ")
	if err != nil || r.Image != "app:1" || len(r.Vulnerabilities) != 3 {
		t.Fatalf("result %+v, %v", r, err)
	}
	args := b.args[len(b.args)-1]
	if args[0] != "sh" || !strings.Contains(args[3], "docker run --rm") || args[4] != trivyImage || args[6] != "app:1" {
		t.Errorf("exec args = %q", args)
	}
}

func TestLocalImages(t *testing.T) {
	b := &scanBackend{Backend: stub.New(), out: "nginx:1.29\n<none>:<none>\naquasec/trivy:0.76.2\nalpine:3.24\n"}
	c := NewController(b, nil)
	c.Start(context.Background())
	got, err := c.LocalImages(context.Background())
	if err != nil || !slices.Equal(got, []string{"alpine:3.24", "nginx:1.29"}) {
		t.Fatalf("images = %v, %v", got, err)
	}
}
