package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Cleanup of unused resources in the VM (I-22). What gets removed depends on
// what removing costs:
//   - build cache unused for a while and untagged images: removed
//     automatically, they are rebuilt or pulled again when needed;
//   - long stopped containers and images no container uses: offered as
//     candidates, removed only when the user picks them;
//   - volumes: only shown. They hold data that cannot be recovered.

const (
	// autoCleanupEvery is how often the automatic cleanup runs.
	autoCleanupEvery = 24 * time.Hour
	// buildCacheMaxIdle is how long build cache may stay unused.
	buildCacheMaxIdle = "720h"
	// stoppedFor is how long a container must be stopped to be a candidate.
	stoppedFor = 7 * 24 * time.Hour
)

var autoCleanupScript = []string{"sh", "-c",
	`docker image prune --force && docker builder prune --force --filter until=` + buildCacheMaxIdle}

// AutoCleanup removes the untagged images and the build cache unused for 30
// days, and returns the space reclaimed.
func (c *Controller) AutoCleanup(ctx context.Context) (int64, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return 0, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, autoCleanupScript)
	if err != nil {
		return 0, err
	}
	if res.ExitCode != 0 {
		return 0, fmt.Errorf("自動の整理に失敗しました: %s", strings.TrimSpace(lastLine(res.Stderr+res.Stdout)))
	}
	var freed int64
	for _, l := range lines(res.Stdout) {
		if size, ok := strings.CutPrefix(l, "Total reclaimed space:"); ok {
			freed += parseSize(size)
		} else if size, ok := strings.CutPrefix(l, "Total:"); ok { // docker builder prune
			freed += parseSize(size)
		}
	}
	return freed, nil
}

// MaybeAutoCleanup runs AutoCleanup when the engine runs (not stopped for
// being idle) and the last run is at least a day ago. Call it periodically.
func (c *Controller) MaybeAutoCleanup(ctx context.Context) {
	if c.Snapshot().Status != vm.StatusRunning || c.sleeping() {
		return
	}
	c.mu.Lock()
	due := time.Since(c.lastCleanup) >= autoCleanupEvery
	if due {
		c.lastCleanup = time.Now()
	}
	c.mu.Unlock()
	if !due {
		return
	}
	freed, err := c.AutoCleanup(ctx)
	if err != nil {
		c.log.Warn("auto cleanup", "err", err)
		return
	}
	c.log.Info("auto cleanup", "freed", FormatBytes(freed))
}

// CleanupItem is a container, image or volume in the cleanup report.
type CleanupItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Project   string `json:"project"` // Compose project, if any
	SizeBytes int64  `json:"sizeBytes"`
	// Detail explains why it is listed: since when a container is stopped,
	// that an image was built locally, whether a volume is in use.
	Detail string `json:"detail"`
	InUse  bool   `json:"inUse"` // volumes: a container uses it
}

// CleanupReport lists what could be removed.
type CleanupReport struct {
	Containers []CleanupItem `json:"containers"` // stopped for 7 days or more
	Images     []CleanupItem `json:"images"`     // no container uses them
	Volumes    []CleanupItem `json:"volumes"`    // shown only, never removed here
	// BuildCacheBytes is the whole build cache; it is cleaned automatically.
	BuildCacheBytes int64 `json:"buildCacheBytes"`
}

const cleanupSeparator = "--merry-cleanup--"

var cleanupScript = []string{"sh", "-c", `docker system df -v --format json &&
echo ` + cleanupSeparator + ` &&
docker ps -aq --no-trunc --filter status=exited --filter status=created |
  xargs -r docker inspect --format '{{.Id}}	{{.State.FinishedAt}}	{{.Created}}' &&
echo ` + cleanupSeparator + ` &&
docker image ls -q --no-trunc | sort -u |
  xargs -r docker image inspect --format '{{.Id}}	{{len .RepoDigests}}	{{json .Identity}}'`}

// CleanupCandidates lists the stopped containers and unused images that could
// be removed, and the volumes with their sizes.
func (c *Controller) CleanupCandidates(ctx context.Context) (CleanupReport, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return CleanupReport{}, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, cleanupScript)
	if err != nil {
		return CleanupReport{}, err
	}
	if res.ExitCode != 0 {
		return CleanupReport{}, fmt.Errorf("候補を取得できませんでした: %s", strings.TrimSpace(lastLine(res.Stderr+res.Stdout)))
	}
	return parseCleanup(res.Stdout, time.Now())
}

func parseCleanup(out string, now time.Time) (CleanupReport, error) {
	parts := strings.Split(strings.ReplaceAll(out, "\r", ""), cleanupSeparator+"\n")
	if len(parts) != 3 {
		return CleanupReport{}, errors.New("候補の出力を読めませんでした")
	}
	var df struct {
		Images []struct {
			ID, Repository, Tag, Size, Containers string
		}
		Containers []struct {
			ID, Names, Labels, Size, Status, Image string
		}
		Volumes []struct {
			Name, Labels, Links, Size string
		}
		BuildCache []struct{ Size string }
	}
	if err := json.Unmarshal([]byte(parts[0]), &df); err != nil {
		return CleanupReport{}, fmt.Errorf("候補の出力を読めませんでした: %w", err)
	}
	stoppedAt := map[string]time.Time{}
	for _, l := range lines(parts[1]) {
		f := strings.Split(l, "\t")
		if len(f) != 3 {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, f[1])
		if err != nil || t.Year() < 2000 { // never started: use the creation time
			t, err = time.Parse(time.RFC3339Nano, f[2])
		}
		if err == nil {
			stoppedAt[f[0]] = t
		}
	}
	built := map[string]bool{}
	for _, l := range lines(parts[2]) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 {
			built[f[0]] = builtLocally(f[1], f[2])
		}
	}

	r := CleanupReport{Containers: []CleanupItem{}, Images: []CleanupItem{}, Volumes: []CleanupItem{}}
	for _, ctr := range df.Containers {
		t, ok := stoppedAt[ctr.ID]
		if !ok || now.Sub(t) < stoppedFor {
			continue
		}
		r.Containers = append(r.Containers, CleanupItem{
			ID: ctr.ID, Name: ctr.Names, Project: label(ctr.Labels, "com.docker.compose.project"),
			SizeBytes: parseSize(firstField(ctr.Size)),
			Detail:    fmt.Sprintf("%d 日前から停止(%s)", int(now.Sub(t).Hours()/24), ctr.Image),
		})
	}
	for _, img := range df.Images {
		if img.Containers != "0" || img.Repository == "<none>" {
			continue // in use, or untagged (removed automatically)
		}
		item := CleanupItem{ID: img.ID, Name: img.Repository + ":" + img.Tag, SizeBytes: parseSize(img.Size)}
		if built[img.ID] {
			item.Detail = "手元でビルドしたイメージ(消すとビルドし直しになります)"
		}
		r.Images = append(r.Images, item)
	}
	for _, v := range df.Volumes {
		inUse := v.Links != "0"
		detail := "どのコンテナも使っていません"
		if inUse {
			detail = "コンテナが使っています"
		}
		if v.Name == trivyCache {
			detail = "脆弱性スキャンのデータベース(消しても次のスキャンでダウンロードし直します)"
		}
		r.Volumes = append(r.Volumes, CleanupItem{
			ID: v.Name, Name: v.Name, Project: label(v.Labels, "com.docker.compose.project"),
			SizeBytes: parseSize(v.Size), Detail: detail, InUse: inUse,
		})
	}
	for _, b := range df.BuildCache {
		r.BuildCacheBytes += parseSize(b.Size)
	}
	for _, list := range [][]CleanupItem{r.Containers, r.Images, r.Volumes} {
		sort.SliceStable(list, func(i, j int) bool { return list[i].SizeBytes > list[j].SizeBytes })
	}
	return r, nil
}

// builtLocally tells a built image from a pulled one. With the containerd
// image store, the engine records pulls and builds (Identity, as JSON); without
// it, a built image is one without RepoDigests.
func builtLocally(repoDigests, identity string) bool {
	var id *struct{ Pull, Build []json.RawMessage }
	if json.Unmarshal([]byte(identity), &id) == nil && id != nil {
		return len(id.Build) > 0 && len(id.Pull) == 0
	}
	return repoDigests == "0"
}

// label reads one label from the "k=v,k=v" form docker prints.
func label(labels, key string) string {
	for _, kv := range strings.Split(labels, ",") {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// firstField drops the " (virtual 13MB)" part of a container size.
func firstField(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

var engineID = regexp.MustCompile(`^(sha256:)?[0-9a-f]{12,64}$`)

// RemoveUnused removes the containers and images the user picked from the
// cleanup report. Containers go first, so that images they used can go too.
// Running containers and images still in use are refused by the engine.
func (c *Controller) RemoveUnused(ctx context.Context, containers, images []string) error {
	if c.Snapshot().Status != vm.StatusRunning {
		return vm.ErrNotRunning
	}
	for _, id := range append(append([]string{}, containers...), images...) {
		if !engineID.MatchString(id) {
			return fmt.Errorf("ID が正しくありません: %q", id)
		}
	}
	var errs []error
	run := func(args ...string) {
		res, err := c.backend.Exec(ctx, args)
		if err == nil && res.ExitCode != 0 {
			err = errors.New(strings.TrimSpace(res.Stderr))
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(containers) > 0 {
		run(append([]string{"docker", "container", "rm"}, containers...)...)
	}
	if len(images) > 0 {
		run(append([]string{"docker", "image", "rm"}, images...)...)
	}
	if len(errs) > 0 {
		return fmt.Errorf("一部を削除できませんでした: %w", errors.Join(errs...))
	}
	return nil
}
