package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

const dfOutput = `{"Images":[
 {"ID":"sha256:aaa","Repository":"nginx","Tag":"1.29","Size":"190MB","Containers":"0"},
 {"ID":"sha256:bbb","Repository":"myapp","Tag":"dev","Size":"1.2GB","Containers":"0"},
 {"ID":"sha256:ccc","Repository":"postgres","Tag":"18","Size":"450MB","Containers":"1"},
 {"ID":"sha256:ddd","Repository":"<none>","Tag":"<none>","Size":"13MB","Containers":"0"}],
"Containers":[
 {"ID":"c1","Names":"shop-db-1","Labels":"com.docker.compose.project=shop,com.docker.compose.service=db","Size":"4.1kB (virtual 450MB)","Image":"postgres:18"},
 {"ID":"c2","Names":"recent","Labels":"","Size":"1MB","Image":"alpine"},
 {"ID":"c3","Names":"running","Labels":"","Size":"1MB","Image":"alpine"}],
"Volumes":[
 {"Name":"shop_pgdata","Labels":"com.docker.compose.project=shop","Links":"1","Size":"2.5GB"},
 {"Name":"old","Labels":"","Links":"0","Size":"10MB"}],
"BuildCache":[{"Size":"184MB"},{"Size":"16MB"}]}
` + cleanupSeparator + `
c1	2026-11-01T10:00:00.5Z	2026-10-30T10:00:00Z
c2	2026-11-23T10:00:00Z	2026-11-20T10:00:00Z
` + cleanupSeparator + `
sha256:aaa	1	{"Pull":[{"Repository":"docker.io/library/nginx"}]}
sha256:bbb	1	{"Build":[{"Ref":"k","CreatedAt":"2026-11-23T16:06:00Z"}]}
sha256:ccc	1	null
`

func TestBuiltLocally(t *testing.T) {
	for _, c := range []struct {
		digests, identity string
		want              bool
	}{
		{"1", `{"Pull":[{}]}`, false},
		{"1", `{"Build":[{}]}`, true},
		{"1", `{"Pull":[{}],"Build":[{}]}`, false},
		{"0", "null", true}, // no Identity: by RepoDigests
		{"1", "null", false},
	} {
		if got := builtLocally(c.digests, c.identity); got != c.want {
			t.Errorf("builtLocally(%q, %q) = %v", c.digests, c.identity, got)
		}
	}
}

func TestParseCleanup(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r, err := parseCleanup(dfOutput, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Containers) != 1 || r.Containers[0].Name != "shop-db-1" || r.Containers[0].Project != "shop" ||
		r.Containers[0].SizeBytes != 4100 || !strings.Contains(r.Containers[0].Detail, "23 日前") {
		t.Errorf("containers = %+v", r.Containers)
	}
	if len(r.Images) != 2 || r.Images[0].Name != "myapp:dev" || !strings.Contains(r.Images[0].Detail, "手元でビルド") ||
		r.Images[1].Name != "nginx:1.29" || r.Images[1].Detail != "" {
		t.Errorf("images = %+v", r.Images)
	}
	if len(r.Volumes) != 2 || r.Volumes[0].Name != "shop_pgdata" || !r.Volumes[0].InUse || r.Volumes[1].InUse {
		t.Errorf("volumes = %+v", r.Volumes)
	}
	if r.BuildCacheBytes != 200_000_000 {
		t.Errorf("build cache = %d", r.BuildCacheBytes)
	}
}

type cleanupBackend struct {
	*stub.Backend
	calls [][]string
	out   string
}

func (b *cleanupBackend) Exec(_ context.Context, args []string) (vm.ExecResult, error) {
	b.calls = append(b.calls, args)
	return vm.ExecResult{Stdout: b.out}, nil
}

func TestAutoCleanupOncePerDay(t *testing.T) {
	b := &cleanupBackend{Backend: stub.New(), out: "Deleted Images:\nx\n\nTotal reclaimed space: 13MB\nTotal:\t1.5GB\n"}
	c := NewController(b, nil)
	ctx := context.Background()
	c.MaybeAutoCleanup(ctx) // stopped: nothing
	c.Start(ctx)
	freed, err := c.AutoCleanup(ctx)
	if err != nil || freed != 1_513_000_000 {
		t.Fatalf("freed %d, %v", freed, err)
	}
	b.calls = nil
	c.MaybeAutoCleanup(ctx)
	c.MaybeAutoCleanup(ctx)
	if len(b.calls) != 1 || !strings.Contains(b.calls[0][2], "until=720h") {
		t.Fatalf("calls = %q", b.calls)
	}
}

func TestRemoveUnused(t *testing.T) {
	b := &cleanupBackend{Backend: stub.New()}
	c := NewController(b, nil)
	ctx := context.Background()
	c.Start(ctx)
	if err := c.RemoveUnused(ctx, []string{"--force"}, nil); err == nil {
		t.Error("accepted a flag as an ID")
	}
	id := strings.Repeat("a", 64)
	if err := c.RemoveUnused(ctx, []string{id}, []string{"sha256:" + id}); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 2 || b.calls[0][1] != "container" || b.calls[1][1] != "image" {
		t.Fatalf("calls = %q", b.calls)
	}
}
