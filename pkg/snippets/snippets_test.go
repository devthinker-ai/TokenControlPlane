package snippets_test

import (
	"encoding/json"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/snippets"
)

func TestBuild_ValidJSONAndFieldNames(t *testing.T) {
	cfg, err := snippets.Build("https://gw.example.com", "srv_1", "github", "tcp_abc123")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"claude":   cfg.ClaudeDesktop,
		"cursor":   cfg.Cursor,
		"windsurf": cfg.Windsurf,
	} {
		t.Run(name, func(t *testing.T) {
			if err := snippets.Validate(raw); err != nil {
				t.Fatalf("validate: %v\n%s", err, raw)
			}
			var root map[string]any
			if err := json.Unmarshal([]byte(raw), &root); err != nil {
				t.Fatal(err)
			}
			servers, ok := root["mcpServers"].(map[string]any)
			if !ok || servers["github"] == nil {
				t.Fatalf("missing mcpServers.github: %#v", root)
			}
			entry := servers["github"].(map[string]any)
			if entry["type"] != "streamableHttp" {
				t.Fatalf("type: %v", entry["type"])
			}
			if entry["url"] != "https://gw.example.com/mcp/srv_1" {
				t.Fatalf("url: %v", entry["url"])
			}
			headers := entry["headers"].(map[string]any)
			if headers["Authorization"] != "Bearer tcp_abc123" {
				t.Fatalf("auth: %v", headers["Authorization"])
			}
		})
	}
}
