package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// ProjectUsage is what the running containers of one Compose project use.
// Containers that are not part of a Compose project are grouped under "".
type ProjectUsage struct {
	Project     string  `json:"project"`
	Containers  int     `json:"containers"`
	CPUPercent  float64 `json:"cpuPercent"` // 100 = one CPU
	MemoryBytes int64   `json:"memoryBytes"`
}

// Usage is a snapshot of the resource use in the VM (I-07).
type Usage struct {
	Projects    []ProjectUsage `json:"projects"` // most memory first
	MemoryBytes int64          `json:"memoryBytes"`
	// MemTotalBytes is the memory of the VM as the engine sees it.
	MemTotalBytes int64 `json:"memTotalBytes"`
}

const usageSeparator = "--merry-usage--"

// usageScript gathers everything in one command: each Exec is a round trip
// through the VM's command runner.
var usageScript = []string{"sh", "-c", `docker info --format '{{.MemTotal}}' &&
echo ` + usageSeparator + ` &&
docker ps --format '{{.ID}}	{{.Label "com.docker.compose.project"}}' &&
echo ` + usageSeparator + ` &&
docker stats --no-stream --format '{{.ID}}	{{.CPUPerc}}	{{.MemUsage}}'`}

// ProjectUsage reads the CPU and memory use of the running containers, summed
// per Compose project, with the docker CLI in the VM.
func (c *Controller) ProjectUsage(ctx context.Context) (Usage, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return Usage{}, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, usageScript)
	if err != nil {
		return Usage{}, err
	}
	if res.ExitCode != 0 {
		return Usage{}, fmt.Errorf("使用量を取得できませんでした: %s", lastLine(res.Stderr+res.Stdout))
	}
	return parseUsage(res.Stdout)
}

func parseUsage(out string) (Usage, error) {
	parts := strings.Split(strings.ReplaceAll(out, "\r", ""), usageSeparator+"\n")
	if len(parts) != 3 {
		return Usage{}, errors.New("使用量の出力を読めませんでした")
	}
	var u Usage
	u.MemTotalBytes, _ = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	projectOf := map[string]string{}
	for _, line := range lines(parts[1]) {
		id, project, _ := strings.Cut(line, "\t")
		projectOf[shortID(id)] = project
	}
	byProject := map[string]*ProjectUsage{}
	for _, line := range lines(parts[2]) {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			continue
		}
		project, ok := projectOf[shortID(f[0])]
		if !ok {
			continue // stopped between the two commands
		}
		p := byProject[project]
		if p == nil {
			p = &ProjectUsage{Project: project}
			byProject[project] = p
		}
		p.Containers++
		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(f[1]), "%"), 64)
		p.CPUPercent += cpu
		used, _, _ := strings.Cut(f[2], "/")
		mem := parseSize(used)
		p.MemoryBytes += mem
		u.MemoryBytes += mem
	}
	for _, p := range byProject {
		u.Projects = append(u.Projects, *p)
	}
	sort.Slice(u.Projects, func(i, j int) bool {
		a, b := u.Projects[i], u.Projects[j]
		if a.MemoryBytes != b.MemoryBytes {
			return a.MemoryBytes > b.MemoryBytes
		}
		return a.Project < b.Project
	})
	return u, nil
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// parseSize reads sizes as docker stats prints them: "12.5MiB", "1.2GiB",
// "512kB", "0B".
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		factor float64
	}{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1},
	}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
			if err != nil {
				return 0
			}
			return int64(v * u.factor)
		}
	}
	return 0
}

// Memory pressure: notify once when the containers use this share of the VM
// memory, and again only after the use went below memoryCalm.
const (
	memoryHigh = 0.9
	memoryCalm = 0.8
)

// CheckMemory notifies the user when the containers use most of the VM
// memory, naming the project that uses the most. Call it periodically.
func (c *Controller) CheckMemory(ctx context.Context) {
	if c.Snapshot().Status != vm.StatusRunning {
		return
	}
	u, err := c.ProjectUsage(ctx)
	if err != nil || u.MemTotalBytes <= 0 {
		return
	}
	ratio := float64(u.MemoryBytes) / float64(u.MemTotalBytes)
	c.mu.Lock()
	notify := ratio >= memoryHigh && !c.memoryHigh
	switch {
	case ratio >= memoryHigh:
		c.memoryHigh = true
	case ratio < memoryCalm:
		c.memoryHigh = false
	}
	c.mu.Unlock()
	if !notify {
		return
	}
	top := ""
	if len(u.Projects) > 0 {
		name := u.Projects[0].Project
		if name == "" {
			name = "Compose 以外のコンテナ"
		}
		top = fmt.Sprintf("いちばん多いのは %s(%s)です。", name, FormatBytes(u.Projects[0].MemoryBytes))
	}
	c.notice(fmt.Sprintf("コンテナが VM のメモリの %.0f%% を使っています。%s使っていないプロジェクトを止めるか、設定でメモリを増やしてください。",
		ratio*100, top))
}

// FormatBytes formats a size for people, in binary units.
func FormatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KiB", n>>10)
	}
}
