package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The request log is meant to be shared as evidence, so it must record the
// calls without leaking the token or any credential in a body.
func TestRequestLogRedactsCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"key-1","attributes":{"name":"k","access_key_id":"AKIA1","access_key_secret":"resp-secret-1"}}}`))
	}))
	defer srv.Close()

	logPath := filepath.Join(t.TempDir(), "api.log")
	c := NewClient("tok-very-secret", WithBaseURL(srv.URL), WithRequestLog(logPath))

	body := map[string]any{
		"name":      "k",
		"variables": []any{map[string]any{"key": "APP_KEY", "value": "req-secret-2"}},
	}
	if err := c.do(context.Background(), "POST", "/buckets/b-1/keys", body, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"tok-very-secret", "resp-secret-1", "req-secret-2"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("log contains %q:\n%s", leaked, raw)
		}
	}

	var e requestLogEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("log is not one JSON line: %v\n%s", err, raw)
	}
	if e.Method != "POST" || e.Status != 201 || !strings.HasSuffix(e.URL, "/buckets/b-1/keys") {
		t.Errorf("entry = %s %s %d", e.Method, e.URL, e.Status)
	}
	// Non-secret fields stay readable.
	if !strings.Contains(string(e.Request), "APP_KEY") || !strings.Contains(string(e.Response), "AKIA1") {
		t.Errorf("non-secret fields were redacted: req=%s resp=%s", e.Request, e.Response)
	}
}
