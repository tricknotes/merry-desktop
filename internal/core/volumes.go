package core

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Volume backup and restore (I-03). The VM writes and reads a gzip tar of the
// volume's directory in a host folder it can reach, so no image has to be
// pulled (the company network may not reach a public registry). Only volumes
// of the "local" driver without options are handled: their data is a plain
// directory in the VM.

// VolumeInfo is a volume that can be backed up.
type VolumeInfo struct {
	Name    string `json:"name"`
	Project string `json:"project"` // Compose project, if any
}

// BackupResult says where a backup went.
type BackupResult struct {
	File      string `json:"file"` // name in the backup folder
	SizeBytes int64  `json:"sizeBytes"`
	// InUseBy are running containers using the volume. Their data may be
	// changing while it is copied (a database should be stopped first).
	InUseBy []string `json:"inUseBy"`
}

// volumeName follows the docker rule for volume names.
var volumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// backupFile is the name of a backup made by this app.
var backupFile = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*\.tar\.gz$`)

// BackupFileName names a backup of volume taken at t.
func BackupFileName(volume string, t time.Time) string {
	return volume + "-" + t.Format("20060102-150405") + ".tar.gz"
}

var backupSuffix = regexp.MustCompile(`-\d{8}-\d{6}\.tar\.gz$`)

// IsBackupFile reports whether name looks like a backup made by this app.
func IsBackupFile(name string) bool { return backupFile.MatchString(name) }

// VolumeFromBackup guesses the volume a backup file was made from.
func VolumeFromBackup(file string) string {
	if !backupSuffix.MatchString(file) {
		return strings.TrimSuffix(file, ".tar.gz")
	}
	return backupSuffix.ReplaceAllString(file, "")
}

var listVolumesScript = []string{"docker", "volume", "ls", "--format",
	`{{.Name}}	{{.Label "com.docker.compose.project"}}`}

// Volumes lists the volumes.
func (c *Controller) Volumes(ctx context.Context) ([]VolumeInfo, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return nil, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, listVolumesScript)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("ボリュームの一覧を取得できませんでした: %s", lastLine(res.Stderr+res.Stdout))
	}
	out := []VolumeInfo{}
	for _, l := range lines(res.Stdout) {
		name, project, _ := strings.Cut(l, "\t")
		out = append(out, VolumeInfo{Name: name, Project: project})
	}
	return out, nil
}

// volumeDirScript prints the directory of a local volume, or fails with a
// message for the user. $1 is the volume.
const volumeDirScript = `dir_of() {
  info=$(docker volume inspect --format '{{.Driver}}	{{len .Options}}	{{.Mountpoint}}' "$1") || return 1
  driver=$(printf '%s' "$info" | cut -f1)
  opts=$(printf '%s' "$info" | cut -f2)
  if [ "$driver" != local ] || [ "$opts" != 0 ]; then
    echo "error: ボリューム $1 は種類が特殊なため扱えません(ドライバー $driver)" >&2
    return 1
  fi
  printf '%s' "$info" | cut -f3
}
`

// backupScript writes the archive next to its final name first, so a failed
// backup never leaves a file that looks complete. $1 volume, $2 directory in
// the VM, $3 file name.
var backupScript = volumeDirScript + `set -e
src=$(dir_of "$1")
mkdir -p "$2"
docker ps --filter volume="$1" --format 'in-use	{{.Names}}'
tar -czf "$2/$3.partial" -C "$src" .
mv "$2/$3.partial" "$2/$3"
echo "size	$(stat -c %s "$2/$3")"
`

// BackupVolume writes a gzip tar of the volume into hostDir (a host folder)
// and returns the file name. A running container that uses the volume does
// not stop the backup; it is reported in InUseBy.
func (c *Controller) BackupVolume(ctx context.Context, volume, hostDir string) (BackupResult, error) {
	if !volumeName.MatchString(volume) {
		return BackupResult{}, fmt.Errorf("ボリューム名が正しくありません: %q", volume)
	}
	dir, err := c.vmPath(hostDir)
	if err != nil {
		return BackupResult{}, err
	}
	file := BackupFileName(volume, time.Now())
	res, err := c.backend.Exec(ctx, []string{"sh", "-c", backupScript, "sh", volume, dir, file})
	if err != nil {
		return BackupResult{}, err
	}
	if res.ExitCode != 0 {
		return BackupResult{}, fmt.Errorf("バックアップできませんでした: %s", userError(res))
	}
	r := BackupResult{File: file, InUseBy: []string{}}
	for _, l := range lines(res.Stdout) {
		k, v, _ := strings.Cut(l, "\t")
		switch k {
		case "in-use":
			r.InUseBy = append(r.InUseBy, v)
		case "size":
			r.SizeBytes, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	return r, nil
}

// restoreScript restores into a new volume, or an existing one that is empty
// and unused, so that no data is ever overwritten. A volume it created is
// removed again when the restore fails. $1 volume, $2 archive in the VM.
var restoreScript = volumeDirScript + `[ -f "$2" ] || { echo "error: バックアップのファイルがありません" >&2; exit 1; }
created=
if docker volume inspect "$1" >/dev/null 2>&1; then
  if [ -n "$(docker ps -a --filter volume="$1" -q)" ]; then
    echo "error: ボリューム $1 はコンテナが使っています。別の名前を指定してください" >&2; exit 1
  fi
else
  docker volume create "$1" >/dev/null || exit 1
  created=1
fi
dst=$(dir_of "$1") || exit 1
if [ -z "$created" ] && [ -n "$(ls -A "$dst")" ]; then
  echo "error: ボリューム $1 は空ではありません。上書きしないので、別の名前を指定してください" >&2; exit 1
fi
if ! tar -xzf "$2" -C "$dst"; then
  [ -n "$created" ] && docker volume rm "$1" >/dev/null
  echo "error: 展開できませんでした" >&2; exit 1
fi
`

// RestoreVolume restores the backup hostFile (in a host folder) into volume.
// The volume must not exist yet, or be empty and unused.
func (c *Controller) RestoreVolume(ctx context.Context, hostFile, volume string) error {
	if !volumeName.MatchString(volume) {
		return fmt.Errorf("ボリューム名が正しくありません: %q", volume)
	}
	file, err := c.vmPath(hostFile)
	if err != nil {
		return err
	}
	if !backupFile.MatchString(path.Base(file)) {
		return fmt.Errorf("バックアップのファイルではありません: %s", hostFile)
	}
	res, err := c.backend.Exec(ctx, []string{"sh", "-c", restoreScript, "sh", volume, file})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("復元できませんでした: %s", userError(res))
	}
	return nil
}

func (c *Controller) vmPath(hostPath string) (string, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return "", vm.ErrNotRunning
	}
	m, ok := c.backend.(vm.HostPathMapper)
	if !ok {
		return "", vm.ErrNotSupported
	}
	return m.VMPath(hostPath)
}

// userError picks the message a script wrote for the user ("error: ..."),
// or the last line of its output.
func userError(res vm.ExecResult) string {
	for _, l := range lines(res.Stderr) {
		if msg, ok := strings.CutPrefix(l, "error: "); ok {
			return msg
		}
	}
	return lastLine(res.Stderr + res.Stdout)
}
