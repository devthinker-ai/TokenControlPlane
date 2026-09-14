package stdio

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ParseArgsJSON validates and returns a string slice from a JSON array column.
func ParseArgsJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("args must be a JSON array of strings")
	}
	return args, nil
}

// ParseEnvJSON validates a JSON object and returns KEY=VALUE pairs for cmd.Env merge.
func ParseEnvJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("env must be a JSON object of string values")
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		if k == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out, nil
}

// PathNotFoundMessage is the exact onboarding error for a missing binary.
func PathNotFoundMessage(command string) string {
	return fmt.Sprintf("command '%s' not found in PATH on the gateway host", command)
}

// ResolveCommand returns the absolute path for a bare command name, or an error
// with PathNotFoundMessage when LookPath fails.
func ResolveCommand(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if filepath.IsAbs(command) {
		if _, err := os.Stat(command); err != nil {
			return "", fmt.Errorf("%s", PathNotFoundMessage(command))
		}
		return command, nil
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("%s", PathNotFoundMessage(command))
	}
	return resolved, nil
}

// DefaultSandboxDir returns ~/.tokencontrolplane/sandbox/<serverID>/ (0700).
func DefaultSandboxDir(serverID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".tokencontrolplane", "sandbox", serverID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// MergeEnviron returns os.Environ() with overlay KEY=VALUE pairs winning.
func MergeEnviron(overlay []string) []string {
	base := os.Environ()
	if len(overlay) == 0 {
		return base
	}
	index := map[string]int{}
	out := make([]string, len(base))
	copy(out, base)
	for i, e := range out {
		if k, _, ok := strings.Cut(e, "="); ok {
			index[k] = i
		}
	}
	for _, e := range overlay {
		k, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if i, exists := index[k]; exists {
			out[i] = e
		} else {
			index[k] = len(out)
			out = append(out, e)
		}
	}
	return out
}
