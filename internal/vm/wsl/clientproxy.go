package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tricknotes/merry-desktop/internal/dockerconfig"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

var _ vm.ClientProxyConfigurer = (*Backend)(nil)

// Scripts that read and write ~/.docker/config.json in a distro as its
// default user, through wsl.exe (no file sharing needed).
const (
	readDockerConfig  = `cat "$HOME/.docker/config.json" 2>/dev/null; true`
	writeDockerConfig = `f="$HOME/.docker/config.json"
mkdir -p "$HOME/.docker" &&
{ [ ! -f "$f" ] || cp "$f" "$f.bak-$1"; } &&
cat > "$f.tmp" && mv "$f.tmp" "$f"`
)

var resultLabels = map[dockerconfig.Result]string{
	dockerconfig.Written:    "書き込みました",
	dockerconfig.Removed:    "取り除きました",
	dockerconfig.Unchanged:  "設定済みです",
	dockerconfig.UsersOwn:   "利用者が設定したプロキシがあるため、変更しません",
	dockerconfig.NotPresent: "設定はありません",
}

// ConfigureClientProxy edits proxies.default in the Windows user's docker
// config and in each integrated distro's, backing up a file before changing it.
func (b *Backend) ConfigureClientProxy(ctx context.Context, p vm.ClientProxy, enable bool) ([]string, error) {
	want := dockerconfig.Proxy{HTTPProxy: p.HTTPProxy, HTTPSProxy: p.HTTPSProxy, NoProxy: p.NoProxy}
	stamp := b.now().Format("20060102-150405")
	var lines []string
	var errs []error

	if path := b.HostDockerConfig; path != "" {
		res, err := editHostDockerConfig(path, want, enable, stamp)
		lines, errs = report(lines, errs, "Windows", res, err)
	}

	b.mu.Lock()
	distros := append([]string(nil), b.integrated...)
	b.mu.Unlock()
	for _, d := range distros {
		res, err := b.editDistroDockerConfig(ctx, d, want, enable, stamp)
		lines, errs = report(lines, errs, d, res, err)
	}
	return lines, errors.Join(errs...)
}

func report(lines []string, errs []error, where string, res dockerconfig.Result, err error) ([]string, []error) {
	if err != nil {
		return append(lines, where+": 失敗しました: "+err.Error()), append(errs, fmt.Errorf("%s: %w", where, err))
	}
	return append(lines, where+": "+resultLabels[res]), errs
}

func editHostDockerConfig(path string, p dockerconfig.Proxy, enable bool, stamp string) (dockerconfig.Result, error) {
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	out, res, err := dockerconfig.Apply(current, p, enable)
	if err != nil || (res != dockerconfig.Written && res != dockerconfig.Removed) {
		return res, err
	}
	if len(current) > 0 {
		if err := os.WriteFile(path+".bak-"+stamp, current, 0o600); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return res, writeFileAtomic(path, out)
}

func (b *Backend) editDistroDockerConfig(ctx context.Context, distro string, p dockerconfig.Proxy, enable bool, stamp string) (dockerconfig.Result, error) {
	cur, err := b.Runner.Run(ctx, "--distribution", distro, "--exec", "sh", "-c", readDockerConfig)
	if err != nil {
		return "", err
	}
	if cur.ExitCode != 0 {
		return "", fmt.Errorf("read config.json: %s", cur.Stderr)
	}
	out, res, err := dockerconfig.Apply([]byte(cur.Stdout), p, enable)
	if err != nil || (res != dockerconfig.Written && res != dockerconfig.Removed) {
		return res, err
	}
	if err := b.mustRun(ctx, RunOptions{Stdin: out},
		"--distribution", distro, "--exec", "sh", "-c", writeDockerConfig, "sh", stamp); err != nil {
		return "", err
	}
	return res, nil
}
