package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// meteringAccessLog combines metering + structured access log so bytes_in/out
// are available without double-wrapping. Never logs request/response bodies.
// Best-effort tools/call name extraction from the buffered request (not the response stream).
func meteringAccessLog(m *Meter, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := auth.KeyFromContext(r.Context())
			start := time.Now()

			var buf bytes.Buffer
			cr := &countingReader{r: io.TeeReader(r.Body, &buf)}
			r.Body = nopCloser{cr}
			cw := newCountingResponseWriter(w)

			next.ServeHTTP(cw, r)

			bytesIn, bytesOut := cr.n, cw.n
			if ok {
				m.RecordForServer(key.ID, chi.URLParam(r, "server_id"), bytesIn, bytesOut, 1)
				maybeLogToolCall(m.store, key, chi.URLParam(r, "server_id"), buf.Bytes())
			}

			keyID := ""
			if ok {
				keyID = key.ID
			}
			logger.Info("proxy request",
				"key_id", keyID,
				"server_id", chi.URLParam(r, "server_id"),
				"status", cw.status,
				"bytes_in", bytesIn,
				"bytes_out", bytesOut,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

type nopCloser struct{ *countingReader }

func (n nopCloser) Close() error { return nil }

func maybeLogToolCall(st *store.Store, key *auth.CachedKey, serverID string, body []byte) {
	if st == nil || key == nil || len(body) == 0 || len(body) > 1<<20 {
		return
	}
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return
	}
	if msg.Method != "tools/call" || msg.Params.Name == "" {
		return
	}
	ctx := context.Background()
	acctID, err := st.AccountIDForKey(ctx, key.ID)
	if err != nil || acctID == "" {
		return
	}
	_ = st.InsertToolCall(ctx, store.ToolCall{
		ID:        uuid.NewString(),
		AccountID: acctID,
		ServerID:  serverID,
		KeyID:     key.ID,
		ToolName:  msg.Params.Name,
	})
}
