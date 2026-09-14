package update_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/update"
)

func TestCheckSameAndNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","assets":[]}`))
	}))
	t.Cleanup(srv.Close)

	info, err := update.Check(update.Config{
		CurrentVersion: "1.1.0",
		UpdateURL:      srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("same version should not be available")
	}

	info, err = update.Check(update.Config{
		CurrentVersion: "1.0.0",
		UpdateURL:      srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != "1.1.0" {
		t.Fatalf("%+v", info)
	}
}

func TestCheckOfflineSilent(t *testing.T) {
	t.Setenv("NO_UPDATE_CHECK", "1")
	info, err := update.Check(update.Config{CurrentVersion: "1.0.0", UpdateURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("NO_UPDATE_CHECK should force available=false")
	}
}

func TestApplyFileSHA256(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokencontrolplane")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	neu := filepath.Join(dir, "newbin")
	payload := []byte("new-binary-contents")
	if err := os.WriteFile(neu, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	if err := update.ApplyFile(exe, neu, "deadbeef"); err == nil {
		t.Fatal("wrong sha should refuse")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatal("binary should be untouched on mismatch")
	}

	if err := update.ApplyFile(exe, neu, hexSum); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(exe + ".prev"); err != nil {
		t.Fatal("expected .prev backup")
	}
	if err := update.Rollback(exe, false, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatalf("rollback got %q", got)
	}
}

func TestRollbackSchemaAdvanced(t *testing.T) {
	err := update.Rollback("/tmp/x", true, []int{1, 2})
	if err == nil || !strings.Contains(err.Error(), "schema has advanced") {
		t.Fatalf("%v", err)
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"1.0.0-rc1", "1.0.0", true},
		{"1.0.0", "1.0.0-rc1", false},
		{"1.0.0", "1.1.0", true},
		{"1.9.0", "1.10.0", true},
		{"1.10.0", "1.9.0", false},
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.0.0", false},
		{"v1.2.3", "1.2.4", true},
		{"1.1.0-rc.1", "1.1.0", true},
		{"1.0.0-rc1", "1.0.0-rc2", true},
	}
	for _, tc := range cases {
		if got := update.VersionLess(tc.a, tc.b); got != tc.less {
			t.Errorf("VersionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.less)
		}
	}
}

func TestFetchRelease(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.URL.Path, "/tags/v1.2.0") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","body":"## Fixes\n- one","published_at":"2026-01-02T03:04:05Z"}`))
	}))
	t.Cleanup(srv.Close)

	notes, err := update.FetchRelease(update.Config{
		UpdateURL:  srv.URL + "/releases/latest",
		HTTPClient: srv.Client(),
	}, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if notes.Tag != "v1.2.0" || !strings.Contains(notes.Body, "Fixes") {
		t.Fatalf("%+v", notes)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestExtractChangelogSection(t *testing.T) {
	md := `# Changelog

## [Unreleased]

- wip

## [1.0.0] - 2026-09-01

### Added
- setup wizard

## [0.9.0] - 2026-08-01

- older
`
	sec, err := update.ExtractChangelogSection(md, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sec, "setup wizard") || strings.Contains(sec, "0.9.0") || strings.Contains(sec, "Unreleased") {
		t.Fatalf("bad section: %s", sec)
	}
	_, err = update.ExtractChangelogSection(md, "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("want missing entry error, got %v", err)
	}
}

func TestExtractChangelogScriptSeeded(t *testing.T) {
	root := filepath.Join("..", "..")
	script := filepath.Join(root, "scripts", "extract-changelog.sh")
	changelog := filepath.Join(root, "CHANGELOG.md")
	if _, err := os.Stat(script); err != nil {
		t.Skip("script not found (module path)")
	}
	out, err := exec.Command("bash", script, "v1.0.0", changelog).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(string(out), "## [1.0.0]") {
		t.Fatalf("unexpected output: %s", out)
	}
	out, err = exec.Command("bash", script, "v9.9.9", changelog).CombinedOutput()
	if err == nil {
		t.Fatal("expected missing section to fail")
	}
	if !strings.Contains(string(out), "no entry") {
		t.Fatalf("%s", out)
	}
}
