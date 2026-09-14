package catalog_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/catalog"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestIndexErrorTruncatesHugeHTMLBody(t *testing.T) {
	huge := strings.Repeat("<html>error page line\n", 400) // >> 300 chars, multi-line
	if len(huge) < 5000 {
		t.Fatalf("fixture too small: %d", len(huge))
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(huge))
	}))
	t.Cleanup(up.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srvID := "srv_html"
	if err := st.CreateServer(t.Context(), store.MCPServer{
		ID: srvID, AccountID: store.DefaultAccountID, Name: "html-up",
		BaseURL: up.URL, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	idx := catalog.NewIndexer(st, nil)
	_, indexErr := idx.IndexServer(srvID)
	if indexErr == "" {
		t.Fatal("expected indexing error")
	}
	if strings.Contains(indexErr, "\n") || strings.Contains(indexErr, "\r") {
		t.Fatalf("indexErr must be single line: %q", indexErr)
	}
	runes := []rune(indexErr)
	if len(runes) > 301 { // 300 runes + ellipsis
		t.Fatalf("indexErr runes=%d: %q", len(runes), indexErr)
	}
	got, err := st.GetServer(t.Context(), srvID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastIndexError != indexErr {
		t.Fatalf("stored=%q returned=%q", got.LastIndexError, indexErr)
	}
}
