// Package updater keeps the running binary current with GitHub releases of the
// fork, without any build toolchain on the device.
//
// On-disk layout (InstallRoot, default /opt/reeftank-hub):
//
//	releases/hub-v0.1.0/reeftank-hub
//	releases/hub-v0.2.0/reeftank-hub
//	current            -> releases/hub-v0.2.0        (symlink; systemd ExecStart)
//	data/                                            (state; never touched here)
//	state/PREVIOUS      hub-v0.1.0                     (rollback target)
//	state/CONFIRMED     hub-v0.2.0                     (last version that passed self-check)
//
// Flow: Check() asks the GitHub API for the newest hub-v* release on the
// configured channel. Apply() downloads the linux/arm64 asset, verifies it
// against SHA256SUMS, unpacks it into releases/<tag>/, atomically repoints
// current, records PREVIOUS, and restarts the service. After restart the new
// build self-confirms (ConfirmAfterStart); an OnFailure systemd unit rolls the
// symlink back to PREVIOUS if it never confirms.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	assetName  = "reeftank-hub-linux-arm64"
	sumsName   = "SHA256SUMS"
	tagPrefix  = "hub-v"
	binaryName = "reeftank-hub"
)

// apiBaseForTest is the GitHub API root; overridden in tests.
var apiBaseForTest = "https://api.github.com"

type Options struct {
	Repo        string // "owner/name"
	Channel     string // "stable" (default) | "prerelease"
	InstallRoot string // e.g. /opt/reeftank-hub
	CurrentTag  string // running version tag, e.g. "hub-v0.1.0" ("dev" disables apply)
	HTTPClient  *http.Client
	// RestartFunc runs the service restart. Default: `systemctl restart reeftank-hub`.
	RestartFunc func(ctx context.Context) error
	// PreApply runs after download/checksum verification and before the release
	// symlink is changed. ReefTank Hub uses it for an SQLite snapshot.
	PreApply func(ctx context.Context, targetTag string) error
}

type Release struct {
	Tag        string
	Prerelease bool
	Notes      string
	AssetURL   string
	SumsURL    string
}

type Updater struct {
	o Options
}

func New(o Options) *Updater {
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if o.Channel == "" {
		o.Channel = "stable"
	}
	if o.RestartFunc == nil {
		o.RestartFunc = systemctlRestart
	}
	return &Updater{o: o}
}

func (u *Updater) dir(sub string) string { return filepath.Join(u.o.InstallRoot, sub) }

// Check returns the newest release strictly newer than CurrentTag, or nil.
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	if u.o.Repo == "" {
		return nil, fmt.Errorf("updater: empty repo")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/repos/%s/releases?per_page=20", apiBaseForTest, u.o.Repo), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.o.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: list releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: list releases: HTTP %d", resp.StatusCode)
	}

	var raw []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Body       string `json:"body"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("updater: decode releases: %w", err)
	}

	var best *Release
	for _, r := range raw {
		if r.Draft || !strings.HasPrefix(r.TagName, tagPrefix) {
			continue
		}
		if r.Prerelease && u.o.Channel != "prerelease" {
			continue
		}
		var rel Release
		rel.Tag = r.TagName
		rel.Prerelease = r.Prerelease
		rel.Notes = r.Body
		for _, a := range r.Assets {
			switch a.Name {
			case assetName:
				rel.AssetURL = a.URL
			case sumsName:
				rel.SumsURL = a.URL
			}
		}
		if rel.AssetURL == "" || rel.SumsURL == "" {
			continue
		}
		if best == nil || cmpTag(rel.Tag, best.Tag) > 0 {
			b := rel
			best = &b
		}
	}
	if best == nil {
		return nil, nil
	}
	if u.o.CurrentTag != "dev" && cmpTag(best.Tag, u.o.CurrentTag) <= 0 {
		return nil, nil
	}
	return best, nil
}

// Apply downloads, verifies, installs, repoints current, and restarts.
func (u *Updater) Apply(ctx context.Context, rel *Release) error {
	if u.o.CurrentTag == "dev" {
		return fmt.Errorf("updater: refusing to self-update a dev build")
	}
	slog.Info("updater: applying", "from", u.o.CurrentTag, "to", rel.Tag)

	bin, err := u.download(ctx, rel.AssetURL)
	if err != nil {
		return err
	}
	sums, err := u.download(ctx, rel.SumsURL)
	if err != nil {
		return err
	}
	want, err := shaFor(sums, assetName)
	if err != nil {
		return err
	}
	got := sha256.Sum256(bin)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("updater: sha256 mismatch for %s", assetName)
	}
	if u.o.PreApply != nil {
		if err := u.o.PreApply(ctx, rel.Tag); err != nil {
			return fmt.Errorf("updater: pre-apply backup: %w", err)
		}
	}

	relDir := u.dir(filepath.Join("releases", rel.Tag))
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		return err
	}
	binPath := filepath.Join(relDir, binaryName)
	tmp := binPath + ".tmp"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, binPath); err != nil {
		return err
	}

	if err := os.MkdirAll(u.dir("state"), 0o755); err != nil {
		return err
	}
	if u.o.CurrentTag != "" && u.o.CurrentTag != "dev" {
		_ = os.WriteFile(u.dir("state/PREVIOUS"), []byte(u.o.CurrentTag+"\n"), 0o644)
	}
	if err := atomicSymlink(relDir, u.dir("current")); err != nil {
		return fmt.Errorf("updater: repoint current: %w", err)
	}

	slog.Info("updater: installed, restarting service", "tag", rel.Tag)
	return u.o.RestartFunc(ctx)
}

// ConfirmAfterStart, called once at daemon startup, marks the running tag as
// good after it has served health for `grace`, so the rollback unit stands down.
func (u *Updater) ConfirmAfterStart(ctx context.Context, runningTag string, grace time.Duration, healthy func() bool) {
	if runningTag == "dev" || u.o.InstallRoot == "" {
		return
	}
	if b, _ := os.ReadFile(u.dir("state/CONFIRMED")); strings.TrimSpace(string(b)) == runningTag {
		return
	}
	go func() {
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if healthy == nil || healthy() {
			_ = os.WriteFile(u.dir("state/CONFIRMED"), []byte(runningTag+"\n"), 0o644)
			slog.Info("updater: version confirmed healthy", "tag", runningTag)
		}
	}()
}

func (u *Updater) download(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := u.o.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func systemctlRestart(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "reeftank-hub")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart: %v: %s", err, out)
	}
	return nil
}

func atomicSymlink(target, link string) error {
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, link)
}

func shaFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("updater: %s not listed in SHA256SUMS", name)
}

// cmpTag compares "hub-vX.Y.Z" tags. Returns -1, 0, 1. Unparseable sorts low.
func cmpTag(a, b string) int {
	pa, pb := parseTag(a), parseTag(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseTag(t string) [3]int {
	t = strings.TrimPrefix(t, tagPrefix)
	t = strings.SplitN(t, "-", 2)[0] // drop any -rc.1 suffix
	var out [3]int
	for i, p := range strings.SplitN(t, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
