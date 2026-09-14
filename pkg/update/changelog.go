package update

import (
	"fmt"
	"os"
	"strings"
)

// ExtractChangelogSection returns the body under "## [X.Y.Z]" (or "## [vX.Y.Z]")
// until the next "## " heading. Tag may include or omit the leading "v".
func ExtractChangelogSection(markdown, tag string) (string, error) {
	ver := stripV(tag)
	wantA := "## [" + ver + "]"
	wantB := "## [v" + ver + "]"
	lines := strings.Split(markdown, "\n")
	start := -1
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, wantA) || strings.HasPrefix(trim, wantB) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("CHANGELOG.md has no entry for %s — add one", ensureV(ver))
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "## ") {
			end = i
			break
		}
	}
	section := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if section == "" {
		return "", fmt.Errorf("CHANGELOG.md has no entry for %s — add one", ensureV(ver))
	}
	return section + "\n", nil
}

// ExtractChangelogFile reads path and extracts the section for tag.
func ExtractChangelogFile(path, tag string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ExtractChangelogSection(string(b), tag)
}
