//go:build linux

// Command merry-dev drives the engine VM from inside WSL (for example the
// Ubuntu distro used for development), with the same core and WSL backend as
// the app. It needs nothing installed on Windows, so it works where unsigned
// Windows executables cannot run. Windows-only parts (tray, named pipe, PATH,
// docker.exe) are not covered.
//
//	merry-dev [-name NAME] setup | start | stop | status | resources | diagnostics | ui | unregister
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/wsl"
)

const usage = `usage: merry-dev [-name NAME] [-v] <command> [flags]

commands:
  setup        create the VM (if needed), start it and run hello-world
  start        start the VM and keep it running until Ctrl+C
  stop         stop the VM
  status       show the VM status and resources
  resources    show or change CPUs, memory (.wslconfig) and the disk limit
  diagnostics  write the diagnostic report (zip)
  ui           open the settings and setup windows in the Windows browser
  unregister   delete the VM and everything in it (needs -yes)

The default VM name is merry-dev-engine, separate from the app's VM.
`

func main() {
	global := flag.NewFlagSet("merry-dev", flag.ExitOnError)
	name := global.String("name", "merry-dev-engine", "VM (WSL distro) name")
	verbose := global.Bool("v", false, "log details to stderr")
	global.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = global.Parse(os.Args[1:])
	if global.NArg() == 0 {
		global.Usage()
		os.Exit(2)
	}

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	backend, err := newBackend(*name)
	if err != nil {
		fail(err)
	}
	c := core.NewController(backend, log)
	c.SkipHostIntegration = true

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := global.Arg(0), global.Args()[1:]
	switch cmd {
	case "setup":
		err = setup(ctx, c, backend)
	case "start":
		err = start(ctx, c, backend)
	case "stop":
		err = backend.Stop(ctx)
		if err == nil {
			fmt.Println("停止しました")
		}
	case "status":
		err = status(ctx, c, backend)
	case "resources":
		err = resources(ctx, c, backend, args)
	case "diagnostics":
		err = diagnostics(ctx, c, args)
	case "unregister":
		err = unregister(ctx, backend, args)
	case "ui":
		err = ui(ctx, c, args)
	default:
		global.Usage()
		os.Exit(2)
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "merry-dev:", err)
	os.Exit(1)
}

// newBackend configures the WSL backend for running inside WSL: Windows paths
// for .wslconfig and the VM disk, and wslpath for wsl.exe arguments.
func newBackend(name string) (*wsl.Backend, error) {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return nil, errors.New("wsl.exe が見つかりません。WSL の中で実行してください(Windows との連携が有効なディストリ)")
	}
	profile, err := windowsDir("USERPROFILE")
	if err != nil {
		return nil, err
	}
	localAppData, err := windowsDir("LOCALAPPDATA")
	if err != nil {
		return nil, err
	}
	b := wsl.New(name)
	b.ConfigPath = filepath.Join(profile, ".wslconfig")
	b.InstallRoot = filepath.Join(localAppData, name)
	b.HostPath = func(p string) string {
		out, err := exec.Command("wslpath", "-w", p).Output()
		if err != nil {
			return p
		}
		return strings.TrimSpace(string(out))
	}
	return b, nil
}

// windowsDir returns a Windows folder (%NAME%) as a path inside WSL.
func windowsDir(variable string) (string, error) {
	cmd := exec.Command("cmd.exe", "/c", "echo %"+variable+"%")
	cmd.Dir = "/mnt/c" // a Linux working directory makes cmd.exe print a UNC warning
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read %%%s%%: %w", variable, err)
	}
	win := strings.TrimSpace(string(out))
	if win == "" || strings.Contains(win, "%") {
		return "", fmt.Errorf("%%%s%% is not set", variable)
	}
	linux, err := exec.Command("wslpath", "-u", win).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath %s: %w", win, err)
	}
	return strings.TrimSpace(string(linux)), nil
}

var stepLabels = map[vm.SetupStep]string{
	vm.StepCheckHost: "WSL2 の確認",
	vm.StepNetwork:   "プロキシと証明書",
	vm.StepCreate:    "VM の作成",
	vm.StepStart:     "VM の起動",
	vm.StepVerify:    "動作確認",
}

var stateMarks = map[core.SetupState]string{
	core.SetupRunning: "…", core.SetupDone: "✓", core.SetupSkipped: "–", core.SetupFailed: "✗",
}

func setup(ctx context.Context, c *core.Controller, b *wsl.Backend) error {
	t0 := time.Now()
	err := c.Setup(ctx, func(e core.SetupEvent) {
		fmt.Printf("%s %s: %s\n", stateMarks[e.State], stepLabels[e.Step], e.Message)
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nセットアップが完了しました(%s)。\n", time.Since(t0).Round(time.Second))
	fmt.Printf("この VM を使い続けるには `merry-dev -name %s start` を実行してください。\n", b.Distro)
	return nil
}

// start keeps the VM running while the command runs: WSL stops a distro soon
// after its last session ends, and this process holds the agent session.
func start(ctx context.Context, c *core.Controller, b *wsl.Backend) error {
	t0 := time.Now()
	c.Start(ctx)
	s := c.Snapshot()
	if s.Status != vm.StatusRunning {
		return errors.New(s.Message)
	}
	fmt.Printf("起動しました(%s)。Ctrl+C で終わると、VM もまもなく止まります。\n\n", time.Since(t0).Round(100*time.Millisecond))
	fmt.Printf("この WSL ディストリからは:\n  docker --context %s ps\n", b.Distro)
	fmt.Printf("(context は docker CLI があれば自動で作られます。ソケット: %s)\n\n", "/mnt/wsl/"+b.Distro+"/run/<ディストリ>/docker.sock")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	last := s.Status
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n終了します")
			return nil
		case <-ticker.C:
			c.Refresh(context.WithoutCancel(ctx))
			if now := c.Snapshot(); now.Status != last {
				fmt.Printf("状態: %s %s\n", now.Status, now.Message)
				last = now.Status
			}
		}
	}
}

func status(ctx context.Context, c *core.Controller, b *wsl.Backend) error {
	s, err := b.Status(ctx)
	if err != nil && !errors.Is(err, vm.ErrNotProvisioned) {
		return err
	}
	if errors.Is(err, vm.ErrNotProvisioned) {
		fmt.Printf("%s: まだありません(merry-dev setup で作成します)\n", b.Distro)
		return nil
	}
	fmt.Printf("%s: %s\n", b.Distro, s)
	if err := c.LoadResources(ctx); err != nil {
		return err
	}
	printResources(c.Snapshot().Resources)
	return nil
}

func printResources(r vm.Resources) {
	show := func(n int, unit string) string {
		if n == 0 {
			return "既定"
		}
		return fmt.Sprintf("%d%s", n, unit)
	}
	fmt.Printf("CPU: %s / メモリ: %s / ディスク上限: %s\n", show(r.CPUs, ""), show(r.MemoryMiB, " MiB"), show(r.DiskGiB, " GiB"))
}

func resources(ctx context.Context, c *core.Controller, b *wsl.Backend, args []string) error {
	fs := flag.NewFlagSet("resources", flag.ExitOnError)
	cpus := fs.Int("cpus", -1, "CPUs (0 = WSL default)")
	memory := fs.Int("memory", -1, "memory in MiB (0 = WSL default)")
	disk := fs.Int("disk", -1, "disk limit in GiB (0 = default)")
	restart := fs.Bool("restart", false, "restart now to apply (CPU/memory: shuts down ALL WSL distros)")
	_ = fs.Parse(args)
	if err := c.LoadResources(ctx); err != nil {
		return err
	}
	r := c.Snapshot().Resources
	if *cpus < 0 && *memory < 0 && *disk < 0 {
		printResources(r)
		fmt.Printf(".wslconfig: %s\n", b.ConfigPath)
		return nil
	}
	if *cpus >= 0 {
		r.CPUs = *cpus
	}
	if *memory >= 0 {
		r.MemoryMiB = *memory
	}
	if *disk >= 0 {
		r.DiskGiB = *disk
	}
	plan, err := c.PlanResources(ctx, r)
	if err != nil {
		return err
	}
	if err := c.ApplyResources(ctx, r, *restart); err != nil {
		return err
	}
	printResources(r)
	switch {
	case plan.RestartHost && !*restart:
		fmt.Println("CPU とメモリは、次に WSL 全体が起動したときに反映されます(ほかのディストリにも効きます)")
	case plan.RestartVM && !*restart:
		fmt.Println("ディスク上限は、次に VM を起動するときに反映されます")
	}
	return nil
}

func diagnostics(ctx context.Context, c *core.Controller, args []string) error {
	fs := flag.NewFlagSet("diagnostics", flag.ExitOnError)
	out := fs.String("o", fmt.Sprintf("merry-dev-diagnostics-%s.zip", time.Now().Format("20060102-150405")), "output zip")
	_ = fs.Parse(args)
	if err := c.WriteDiagnostics(ctx, *out, core.DiagnosticsInput{}); err != nil {
		return err
	}
	fmt.Println("書き出しました:", *out)
	return nil
}

func unregister(ctx context.Context, b *wsl.Backend, args []string) error {
	fs := flag.NewFlagSet("unregister", flag.ExitOnError)
	yes := fs.Bool("yes", false, "really delete the VM, its images and volumes")
	_ = fs.Parse(args)
	if !*yes {
		return fmt.Errorf("%s を中のイメージやボリュームごと消します。実行するには -yes を付けてください", b.Distro)
	}
	if err := b.Unregister(ctx); err != nil {
		return err
	}
	fmt.Println("削除しました:", b.Distro)
	return nil
}
