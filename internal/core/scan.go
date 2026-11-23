package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/appinfo"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Image vulnerability scan (I-02). Trivy is not bundled: the scan runs the
// official Trivy image in the VM, pinned by digest, and reads the image from
// the engine. Its vulnerability database is kept in a volume.
const (
	trivyImage = "aquasec/trivy:0.76.2@sha256:8f6bb947dbc528902e11f2f5be52f7b519f6a39d5d148f41166f2040499768fb"
	// maxVulnerabilities bounds the list sent to the UI; Counts stays complete.
	maxVulnerabilities = 500
)

var trivyCache = appinfo.ID + "-trivy-cache"

// Vulnerability is one finding of a scan.
type Vulnerability struct {
	ID        string `json:"id"`
	Package   string `json:"package"`
	Installed string `json:"installed"`
	Fixed     string `json:"fixed"` // "" when no fixed version exists
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Target    string `json:"target"` // the OS or the file the package is in
}

// ScanResult is the outcome of an image scan.
type ScanResult struct {
	Image string `json:"image"`
	// Counts has the number of findings per severity: CRITICAL, HIGH, MEDIUM,
	// LOW and UNKNOWN.
	Counts          map[string]int  `json:"counts"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities"` // most severe first
	Truncated       bool            `json:"truncated"`
}

// scanScript runs Trivy with the docker CLI of the VM. It runs in a login
// shell, which gives it the VM's proxy settings; the proxy variables are passed
// by name so their values (which may hold credentials) are not in the command.
// The VM's CA bundle, with the extra certificates, replaces Trivy's.
const scanScript = `exec docker run --rm \
  -e HTTP_PROXY -e HTTPS_PROXY -e NO_PROXY -e http_proxy -e https_proxy -e no_proxy \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /etc/ssl/certs/ca-certificates.crt:/etc/ssl/certs/ca-certificates.crt:ro \
  -v "$1":/root/.cache \
  "$0" image --image-src docker --scanners vuln --format json --quiet "$2"`

var severityRank = map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3, "UNKNOWN": 4}

// ScanImage scans a local image for known vulnerabilities. The first scan
// downloads the database (some tens of MB) and takes longer.
func (c *Controller) ScanImage(ctx context.Context, image string) (ScanResult, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\n") {
		return ScanResult{}, errors.New("イメージ名が正しくありません")
	}
	if c.Snapshot().Status != vm.StatusRunning {
		return ScanResult{}, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, []string{"sh", "-l", "-c", scanScript, trivyImage, trivyCache, image})
	if err != nil {
		return ScanResult{}, err
	}
	if res.ExitCode != 0 {
		if strings.Contains(res.Stderr, "No such image") {
			return ScanResult{}, fmt.Errorf("イメージ %s はこの VM にありません。先に docker pull してください", image)
		}
		return ScanResult{}, fmt.Errorf("スキャンできませんでした: %s", strings.TrimSpace(lastLine(res.Stderr+res.Stdout)))
	}
	return parseTrivy(image, res.Stdout)
}

func parseTrivy(image, out string) (ScanResult, error) {
	var report struct {
		Results []struct {
			Target          string
			Vulnerabilities []struct {
				VulnerabilityID, PkgName, InstalledVersion, FixedVersion, Severity, Title string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		return ScanResult{}, fmt.Errorf("スキャン結果を読めませんでした: %w", err)
	}
	r := ScanResult{Image: image, Counts: map[string]int{}, Vulnerabilities: []Vulnerability{}}
	for sev := range severityRank {
		r.Counts[sev] = 0
	}
	for _, res := range report.Results {
		for _, v := range res.Vulnerabilities {
			sev := strings.ToUpper(v.Severity)
			if _, ok := severityRank[sev]; !ok {
				sev = "UNKNOWN"
			}
			r.Counts[sev]++
			r.Vulnerabilities = append(r.Vulnerabilities, Vulnerability{
				ID: v.VulnerabilityID, Package: v.PkgName, Installed: v.InstalledVersion,
				Fixed: v.FixedVersion, Severity: sev, Title: v.Title, Target: res.Target,
			})
		}
	}
	sort.SliceStable(r.Vulnerabilities, func(i, j int) bool {
		return severityRank[r.Vulnerabilities[i].Severity] < severityRank[r.Vulnerabilities[j].Severity]
	})
	if len(r.Vulnerabilities) > maxVulnerabilities {
		r.Vulnerabilities = r.Vulnerabilities[:maxVulnerabilities]
		r.Truncated = true
	}
	return r, nil
}

// LocalImages lists the images in the engine as "repository:tag", to pick one
// to scan.
func (c *Controller) LocalImages(ctx context.Context) ([]string, error) {
	if c.Snapshot().Status != vm.StatusRunning {
		return nil, vm.ErrNotRunning
	}
	res, err := c.backend.Exec(ctx, []string{"docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}"})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("イメージの一覧を取得できませんでした: %s", lastLine(res.Stderr))
	}
	images := []string{}
	for _, l := range lines(res.Stdout) {
		if !strings.Contains(l, "<none>") && !strings.HasPrefix(l, "aquasec/trivy:") {
			images = append(images, l)
		}
	}
	sort.Strings(images)
	return images, nil
}
