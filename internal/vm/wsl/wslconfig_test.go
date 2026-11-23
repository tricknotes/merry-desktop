package wsl

import "testing"

func TestParseWSLConfig(t *testing.T) {
	content := "# comment\n[experimental]\nmemory=1GB\n\n[WSL2]\n processors = 6 \nmemory=8GB # inline\nswap=0\n"
	cfg, err := parseWSLConfig(content)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Processors != 6 || cfg.MemoryMiB != 8192 {
		t.Fatalf("cfg = %+v, want 6 CPUs / 8192 MiB (keys outside [wsl2] ignored)", cfg)
	}
	empty, err := parseWSLConfig("")
	if err != nil || empty != (wslConfig{}) {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	if _, err := parseWSLConfig("[wsl2]\nprocessors=many\n"); err == nil {
		t.Fatal("want error for a non-numeric processors")
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int{"8GB": 8192, "4096MB": 4096, "2g": 2048, "1048576KB": 1024, "1073741824": 1024, "1.5GB": 1536}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestUpdateWSLConfig(t *testing.T) {
	cfg := wslConfig{Processors: 4, MemoryMiB: 6144}
	cases := []struct {
		name, in, want string
	}{
		{
			name: "empty file",
			in:   "",
			want: "[wsl2]\nprocessors=4\nmemory=6144MB\n",
		},
		{
			name: "no wsl2 section keeps others",
			in:   "[experimental]\nautoMemoryReclaim=gradual\n",
			want: "[experimental]\nautoMemoryReclaim=gradual\n\n[wsl2]\nprocessors=4\nmemory=6144MB\n",
		},
		{
			name: "replaces values in place and keeps other keys and comments",
			in:   "# mine\n[wsl2]\nmemory=2GB\n# keep\nswap=0\nprocessors=2\n\n[experimental]\nx=1\n",
			want: "# mine\n[wsl2]\nmemory=6144MB\n# keep\nswap=0\nprocessors=4\n\n[experimental]\nx=1\n",
		},
		{
			name: "adds missing key at the end of the section",
			in:   "[wsl2]\nswap=0\n\n[experimental]\nx=1\n",
			want: "[wsl2]\nswap=0\nprocessors=4\nmemory=6144MB\n\n[experimental]\nx=1\n",
		},
		{
			name: "keeps CRLF",
			in:   "[wsl2]\r\nprocessors=2\r\n",
			want: "[wsl2]\r\nprocessors=4\r\nmemory=6144MB\r\n",
		},
		{
			name: "drops duplicate managed keys",
			in:   "[wsl2]\nprocessors=1\nprocessors=2\nmemory=1GB\n",
			want: "[wsl2]\nprocessors=4\nmemory=6144MB\n",
		},
	}
	for _, c := range cases {
		if got := updateWSLConfig(c.in, cfg); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		// Applying again must not change anything.
		if got := updateWSLConfig(updateWSLConfig(c.in, cfg), cfg); got != c.want {
			t.Errorf("%s: not idempotent: %q", c.name, got)
		}
	}
}

func TestUpdateWSLConfigZeroRemovesKey(t *testing.T) {
	in := "[wsl2]\nprocessors=2\nmemory=4GB\nswap=0\n"
	got := updateWSLConfig(in, wslConfig{Processors: 0, MemoryMiB: 2048})
	if want := "[wsl2]\nmemory=2048MB\nswap=0\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := updateWSLConfig("", wslConfig{}); got != "" {
		t.Errorf("all defaults on an empty file = %q, want no change", got)
	}
}
