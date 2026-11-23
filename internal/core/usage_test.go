package core

import (
	"context"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

const usageOutput = "8000000000\n" + usageSeparator + "\n" +
	"aaaaaaaaaaaa\tshop\nbbbbbbbbbbbb\tshop\ncccccccccccc\t\ndddddddddddd\tblog\n" +
	usageSeparator + "\n" +
	"aaaaaaaaaaaa\t12.50%\t1.5GiB / 7.45GiB\n" +
	"bbbbbbbbbbbb\t0.50%\t512MiB / 7.45GiB\n" +
	"cccccccccccc\t1.00%\t10MiB / 7.45GiB\n" +
	"dddddddddddd\t0.00%\t300kB / 7.45GiB\n" +
	"eeeeeeeeeeee\t0.00%\t1MiB / 7.45GiB\n"

func TestParseUsage(t *testing.T) {
	u, err := parseUsage(usageOutput)
	if err != nil {
		t.Fatal(err)
	}
	if u.MemTotalBytes != 8000000000 || len(u.Projects) != 3 {
		t.Fatalf("usage = %+v", u)
	}
	shop := u.Projects[0]
	if shop.Project != "shop" || shop.Containers != 2 || shop.CPUPercent != 13 || shop.MemoryBytes != 2<<30 {
		t.Errorf("shop = %+v", shop)
	}
	if u.Projects[1].Project != "" || u.Projects[2].Project != "blog" || u.Projects[2].MemoryBytes != 300000 {
		t.Errorf("projects = %+v", u.Projects)
	}
	if _, err := parseUsage("garbage"); err == nil {
		t.Error("garbage parsed")
	}
}

type usageBackend struct {
	*stub.Backend
	out string
}

func (b *usageBackend) Exec(context.Context, []string) (vm.ExecResult, error) {
	return vm.ExecResult{Stdout: b.out}, nil
}

func TestCheckMemoryNotifiesOnce(t *testing.T) {
	b := &usageBackend{Backend: stub.New()}
	c := NewController(b, nil)
	c.Start(context.Background())
	var notices []string
	c.OnNotice(func(m string) { notices = append(notices, m) })
	withUse := func(used string) string {
		return "1000000000\n" + usageSeparator + "\naaaaaaaaaaaa\tshop\n" + usageSeparator +
			"\naaaaaaaaaaaa\t1%\t" + used + " / 1GB\n"
	}
	for _, used := range []string{"500MB", "950MB", "960MB", "700MB", "920MB"} {
		b.out = withUse(used)
		c.CheckMemory(context.Background())
	}
	if len(notices) != 2 || !strings.Contains(notices[0], "95%") || !strings.Contains(notices[0], "shop") {
		t.Fatalf("notices = %q", notices)
	}
}
