// Package snippets builds one-click MCP client connection JSON.
package snippets

import (
	"encoding/json"
	"fmt"
)

// ClientConfigs holds ready-to-paste JSON for each supported client.
type ClientConfigs struct {
	ClaudeDesktop string `json:"claude_desktop"`
	Cursor        string `json:"cursor"`
	Windsurf      string `json:"windsurf"`
}

type mcpEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type mcpRoot struct {
	MCPServers map[string]mcpEntry `json:"mcpServers"`
}

// Build returns valid JSON snippets for Claude Desktop, Cursor, and Windsurf.
// gatewayBase should be like "https://gw.example.com" (no trailing slash).
// serverID is the MCP server path segment; serverName is the mcpServers map key.
func Build(gatewayBase, serverID, serverName, apiKey string) (ClientConfigs, error) {
	if serverName == "" {
		serverName = "tokencontrolplane"
	}
	url := fmt.Sprintf("%s/mcp/%s", trimSlash(gatewayBase), serverID)
	root := mcpRoot{
		MCPServers: map[string]mcpEntry{
			serverName: {
				Type: "streamableHttp",
				URL:  url,
				Headers: map[string]string{
					"Authorization": "Bearer " + apiKey,
				},
			},
		},
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return ClientConfigs{}, err
	}
	s := string(b)
	// Same shape for all three clients (official mcpServers config).
	return ClientConfigs{
		ClaudeDesktop: s,
		Cursor:        s,
		Windsurf:      s,
	}, nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Validate parses snippet JSON and checks required field names.
func Validate(raw string) error {
	var root mcpRoot
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return err
	}
	if len(root.MCPServers) == 0 {
		return fmt.Errorf("missing mcpServers")
	}
	for name, e := range root.MCPServers {
		if e.Type != "streamableHttp" {
			return fmt.Errorf("%s: type must be streamableHttp", name)
		}
		if e.URL == "" {
			return fmt.Errorf("%s: missing url", name)
		}
		if e.Headers["Authorization"] == "" {
			return fmt.Errorf("%s: missing Authorization header", name)
		}
	}
	return nil
}
