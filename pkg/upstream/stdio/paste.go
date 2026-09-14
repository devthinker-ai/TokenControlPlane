package stdio

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PasteEntry is one server extracted from an mcp.json paste.
type PasteEntry struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // http|stdio
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	BaseURL   string            `json:"base_url,omitempty"`
}

// ParseMCPJSON extracts server entries from Cursor/Claude mcp.json shapes.
// Accepts:
//   - {"mcpServers": {"name": {"command","args","env"}}}
//   - {"mcpServers": {"name": {"url": "https://..."}}}
//   - bare {"command","args","env"} (single unnamed → name "")
//   - bare {"url": "..."}
func ParseMCPJSON(raw string) ([]PasteEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty paste")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}

	if ms, ok := top["mcpServers"]; ok {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(ms, &servers); err != nil {
			return nil, fmt.Errorf("mcpServers must be an object")
		}
		out := make([]PasteEntry, 0, len(servers))
		for name, body := range servers {
			e, err := parseOneEntry(name, body)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("mcpServers is empty")
		}
		return out, nil
	}

	// Single entry at top level.
	body, _ := json.Marshal(top)
	e, err := parseOneEntry("", body)
	if err != nil {
		return nil, err
	}
	return []PasteEntry{e}, nil
}

func parseOneEntry(name string, body json.RawMessage) (PasteEntry, error) {
	var m struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url"`
		BaseURL string            `json:"base_url"`
		Type    string            `json:"type"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return PasteEntry{}, fmt.Errorf("invalid server entry %q", name)
	}
	url := m.URL
	if url == "" {
		url = m.BaseURL
	}
	if m.Command != "" {
		return PasteEntry{
			Name: name, Transport: "stdio",
			Command: m.Command, Args: m.Args, Env: m.Env,
		}, nil
	}
	if url != "" {
		return PasteEntry{
			Name: name, Transport: "http", BaseURL: url,
		}, nil
	}
	return PasteEntry{}, fmt.Errorf("server %q needs command or url", name)
}
