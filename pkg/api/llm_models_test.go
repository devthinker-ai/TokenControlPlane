package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMultipleRouteAliasesSameUpstream(t *testing.T) {
	h, _ := setupAPI(t)
	tok, _ := registerAdmin(t, h, "multialias@test.local")

	resp := authJSON(t, h, http.MethodPost, "/llm/providers", tok, map[string]any{
		"name": "OpenAI", "base_url": "https://api.openai.com/v1",
		"auth_value": "Bearer sk-test", "default_model": "gpt-4o-mini",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("provider: %d %s", resp.Code, resp.Body.String())
	}
	var prov map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &prov)
	pid, _ := prov["id"].(string)
	if pid == "" {
		t.Fatalf("provider id missing: %v", prov)
	}

	upstream := "gpt-4o-mini"
	aliases := []string{"chat", "fast", "gpt-4o-mini"}
	for _, name := range aliases {
		resp = authJSON(t, h, http.MethodPost, "/llm/models", tok, map[string]any{
			"name": name, "model": upstream, "provider_id": pid,
		})
		if resp.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, resp.Code, resp.Body.String())
		}
		var m map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &m)
		if m["name"] != name || m["model"] != upstream || m["provider_id"] != pid {
			t.Fatalf("route payload=%v", m)
		}
	}

	// Duplicate alias → 409
	resp = authJSON(t, h, http.MethodPost, "/llm/models", tok, map[string]any{
		"name": "chat", "model": upstream, "provider_id": pid,
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("dup want 409, got %d %s", resp.Code, resp.Body.String())
	}

	resp = authJSON(t, h, http.MethodGet, "/llm/models", tok, nil)
	if resp.Code != 200 {
		t.Fatalf("list: %d %s", resp.Code, resp.Body.String())
	}
	var list []map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &list)
	got := map[string]string{}
	for _, m := range list {
		name, _ := m["name"].(string)
		model, _ := m["model"].(string)
		got[name] = model
	}
	for _, name := range aliases {
		if got[name] != upstream {
			t.Fatalf("list missing %s→%s; got=%v", name, upstream, got)
		}
	}
}
