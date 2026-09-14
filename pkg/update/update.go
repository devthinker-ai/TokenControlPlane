// Package update implements self-hosted binary update / check / rollback.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultRepo = "devthinker-ai/TokenControlPlane"

// Info is the result of a version check.
type Info struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checked_at"`
}

// Config for GitHub releases.
type Config struct {
	CurrentVersion string
	UpdateURL      string // override for mirrors; empty → GitHub API
	HTTPClient     *http.Client
	Repo           string // owner/name
}

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Config) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	if r := os.Getenv("GITHUB_REPO"); r != "" {
		return r
	}
	return defaultRepo
}

func (c Config) latestURL() string {
	if c.UpdateURL != "" {
		return strings.TrimRight(c.UpdateURL, "/")
	}
	return "https://api.github.com/repos/" + c.repo() + "/releases/latest"
}

// tagReleaseURL resolves a specific release. When UPDATE_URL mirrors /releases/latest,
// we rewrite the trailing /latest to /tags/{tag}; otherwise append /tags/{tag}.
func (c Config) tagReleaseURL(tag string) string {
	tag = ensureV(tag)
	if c.UpdateURL != "" {
		u := strings.TrimRight(c.UpdateURL, "/")
		if strings.HasSuffix(u, "/latest") {
			return strings.TrimSuffix(u, "/latest") + "/tags/" + tag
		}
		return u + "/tags/" + tag
	}
	return "https://api.github.com/repos/" + c.repo() + "/releases/tags/" + tag
}

func ensureV(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return tag
	}
	if strings.HasPrefix(tag, "v") || strings.HasPrefix(tag, "V") {
		return "v" + stripV(tag)
	}
	return "v" + tag
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// ReleaseNotes is a proxied GitHub (or mirror) release body.
type ReleaseNotes struct {
	Tag         string    `json:"tag"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body_markdown"`
}

// Check returns whether a newer release is available. Network errors return available=false.
func Check(cfg Config) (Info, error) {
	now := time.Now().UTC()
	info := Info{Current: stripV(cfg.CurrentVersion), CheckedAt: now}
	if os.Getenv("NO_UPDATE_CHECK") == "1" {
		info.Latest = info.Current
		return info, nil
	}
	req, err := http.NewRequest(http.MethodGet, cfg.latestURL(), nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tokencontrolplane-update")
	resp, err := cfg.client().Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return info, fmt.Errorf("release API status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, err
	}
	info.Latest = stripV(rel.TagName)
	info.Available = versionLess(info.Current, info.Latest)
	return info, nil
}

// AssetURL finds the platform binary asset download URL on the latest release.
func AssetURL(cfg Config, goos, goarch string) (tag, assetURL, sumsURL string, err error) {
	req, err := http.NewRequest(http.MethodGet, cfg.latestURL(), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tokencontrolplane-update")
	resp, err := cfg.client().Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("release API status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", err
	}
	want := fmt.Sprintf("tokencontrolplane_%s_%s", goos, goarch)
	var binURL, sumURL string
	for _, a := range rel.Assets {
		n := a.Name
		if strings.Contains(n, "SHA256SUMS") {
			sumURL = a.BrowserDownloadURL
		}
		if strings.HasPrefix(n, want) && (strings.HasSuffix(n, ".tar.gz") || !strings.Contains(n, ".")) {
			binURL = a.BrowserDownloadURL
		}
	}
	if binURL == "" {
		return "", "", "", fmt.Errorf("no asset matching %s*", want)
	}
	return stripV(rel.TagName), binURL, sumURL, nil
}

// ApplyFile replaces the running binary with localFile after sha256 verification.
func ApplyFile(exePath, localFile, expectSHA string) error {
	sum, err := fileSHA256(localFile)
	if err != nil {
		return err
	}
	if expectSHA != "" && !strings.EqualFold(sum, strings.TrimSpace(expectSHA)) {
		return fmt.Errorf("sha256 mismatch: got %s want %s (refusing to replace binary)", sum, expectSHA)
	}
	return replaceBinary(exePath, localFile)
}

// DownloadAndApply fetches the platform asset, verifies SHA256SUMS, replaces binary.
func DownloadAndApply(cfg Config, exePath string) error {
	tag, binURL, sumsURL, err := AssetURL(cfg, runtime.GOOS, runtime.GOARCH)
	_ = tag
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exePath), "tokencontrolplane-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := downloadTo(cfg.client(), binURL, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()

	expect := ""
	if sumsURL != "" {
		sums, err := fetchBody(cfg.client(), sumsURL)
		if err != nil {
			return fmt.Errorf("fetch SHA256SUMS: %w", err)
		}
		expect, err = findSum(sums, filepath.Base(binURL))
		if err != nil {
			// try matching platform prefix
			expect, err = findSumPrefix(sums, fmt.Sprintf("tokencontrolplane_%s_%s", runtime.GOOS, runtime.GOARCH))
			if err != nil {
				return err
			}
		}
	}
	sum, err := fileSHA256(tmpPath)
	if err != nil {
		return err
	}
	if expect != "" && !strings.EqualFold(sum, expect) {
		return fmt.Errorf("sha256 mismatch: got %s want %s (refusing to replace binary)", sum, expect)
	}
	return replaceBinary(exePath, tmpPath)
}

// Rollback restores tokencontrolplane.prev over the current binary.
// schemaAdvanced: if true, refuse and print migration guidance.
func Rollback(exePath string, schemaAdvanced bool, applied []int) error {
	if schemaAdvanced {
		return fmt.Errorf("cannot rollback: schema has advanced beyond the previous binary (applied migrations %v). "+
			"Restore a DB backup or keep the newer binary", applied)
	}
	prev := exePath + ".prev"
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no backup at %s", prev)
	}
	return replaceBinary(exePath, prev)
}

// MaybeRestartSystemd restarts the unit if running under systemd.
func MaybeRestartSystemd() (restarted bool, msg string) {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false, "Binary replaced. Restart the process to finish the update."
	}
	cmd := exec.Command("systemctl", "restart", "tokencontrolplane")
	if err := cmd.Run(); err != nil {
		return false, "Binary replaced. systemctl restart tokencontrolplane failed — restart manually."
	}
	return true, "Restarted tokencontrolplane via systemctl."
}

func replaceBinary(exePath, newPath string) error {
	prev := exePath + ".prev"
	absNew, err1 := filepath.Abs(newPath)
	absPrev, err2 := filepath.Abs(prev)
	restoringPrev := err1 == nil && err2 == nil && absNew == absPrev

	if restoringPrev {
		// Rollback: move current aside, then rename .prev into place.
		// Must not os.Remove(prev) first — that is the restore source.
		tmp := exePath + ".rollback-tmp"
		_ = os.Remove(tmp)
		if err := os.Rename(exePath, tmp); err != nil && !os.IsNotExist(err) {
			if err2 := copyFile(exePath, tmp); err2 != nil {
				return fmt.Errorf("stash current binary: %w", err)
			}
			_ = os.Remove(exePath)
		}
		if err := os.Rename(prev, exePath); err != nil {
			_ = os.Rename(tmp, exePath)
			return err
		}
		_ = os.Remove(tmp)
		_ = os.Chmod(exePath, 0o755)
		return nil
	}

	_ = os.Remove(prev)
	if err := os.Rename(exePath, prev); err != nil && !os.IsNotExist(err) {
		// Copy fallback if rename across devices — still try.
		if err2 := copyFile(exePath, prev); err2 != nil {
			return fmt.Errorf("backup current binary: %w", err)
		}
		_ = os.Remove(exePath)
	}
	if err := copyFile(newPath, exePath); err != nil {
		// Attempt restore
		_ = os.Rename(prev, exePath)
		return err
	}
	_ = os.Chmod(exePath, 0o755)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func downloadTo(c *http.Client, url string, w io.Writer) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func fetchBody(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func findSum(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no sha256 entry for %s", name)
}

func findSumPrefix(sums, prefix string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.HasPrefix(fields[len(fields)-1], prefix) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no sha256 entry for prefix %s", prefix)
}

func stripV(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// VersionLess reports whether a < b using SemVer-ish dotted numeric comparison.
// Pre-releases (suffix after '-') sort below the matching release:
// 1.0.0-rc1 < 1.0.0. Numeric segments compare numerically (1.9.0 < 1.10.0).
func VersionLess(a, b string) bool {
	pa, pb := parseVersion(a), parseVersion(b)
	n := len(pa.nums)
	if len(pb.nums) > n {
		n = len(pb.nums)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(pa.nums) {
			ai = pa.nums[i]
		}
		if i < len(pb.nums) {
			bi = pb.nums[i]
		}
		if ai < bi {
			return true
		}
		if ai > bi {
			return false
		}
	}
	// Numeric equal — SemVer: presence of pre-release makes it older.
	if pa.pre == pb.pre {
		return false
	}
	if pa.pre != "" && pb.pre == "" {
		return true
	}
	if pa.pre == "" && pb.pre != "" {
		return false
	}
	return pa.pre < pb.pre
}

type parsedVersion struct {
	nums []int
	pre  string // empty = release
}

func parseVersion(v string) parsedVersion {
	v = stripV(v)
	core, pre, _ := strings.Cut(v, "-")
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(pre, '+'); i >= 0 {
		pre = pre[:i]
	}
	parts := strings.Split(core, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		var n int
		fmt.Sscanf(p, "%d", &n)
		nums = append(nums, n)
	}
	return parsedVersion{nums: nums, pre: pre}
}

func versionLess(a, b string) bool { return VersionLess(a, b) }

// FetchRelease returns release notes for a tag. Honors NO_UPDATE_CHECK (returns error).
func FetchRelease(cfg Config, tag string) (ReleaseNotes, error) {
	out := ReleaseNotes{Tag: ensureV(tag)}
	if os.Getenv("NO_UPDATE_CHECK") == "1" {
		return out, fmt.Errorf("update checks disabled (NO_UPDATE_CHECK=1)")
	}
	req, err := http.NewRequest(http.MethodGet, cfg.tagReleaseURL(tag), nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tokencontrolplane-update")
	resp, err := cfg.client().Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("release API status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return out, err
	}
	out.Tag = ensureV(rel.TagName)
	if out.Tag == "v" {
		out.Tag = ensureV(tag)
	}
	out.PublishedAt = rel.PublishedAt
	out.Body = rel.Body
	return out, nil
}
