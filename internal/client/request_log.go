package client

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// WithRequestLog appends one JSON line per API call to the file at path:
// time, method, URL, status, duration, and the request and response bodies
// with secret values redacted. The Authorization header is never written.
//
// It exists so a person testing the provider can see exactly which calls a
// plan or apply made. Several provider processes may write to the same file,
// so each line is written with a single append.
func WithRequestLog(path string) ClientOption {
	return func(c *Client) { c.requestLog = path }
}

// redactedKeys are JSON keys whose values are credentials, wherever they
// appear in a request or response body.
var redactedKeys = map[string]bool{
	"password":          true,
	"secret":            true,
	"access_key_secret": true,
	"token":             true,
	"nightwatch_token":  true,
	"deploy_hook_url":   true,
	// Environment variable and secret values.
	"value": true,
}

const maxLoggedBody = 64 << 10

var requestLogMu sync.Mutex

type requestLogEntry struct {
	Time       string          `json:"time"`
	Method     string          `json:"method"`
	URL        string          `json:"url"`
	Status     int             `json:"status,omitempty"`
	DurationMS int64           `json:"duration_ms"`
	Error      string          `json:"error,omitempty"`
	Request    json.RawMessage `json:"request,omitempty"`
	Response   json.RawMessage `json:"response,omitempty"`
}

func (c *Client) logRequest(start time.Time, method, url string, status int, reqBody, respBody []byte, callErr error) {
	if c.requestLog == "" {
		return
	}
	e := requestLogEntry{
		Time:       start.UTC().Format(time.RFC3339Nano),
		Method:     method,
		URL:        url,
		Status:     status,
		DurationMS: time.Since(start).Milliseconds(),
		Request:    redactBody(reqBody),
		Response:   redactBody(respBody),
	}
	if callErr != nil {
		e.Error = callErr.Error()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}

	requestLogMu.Lock()
	defer requestLogMu.Unlock()
	f, err := os.OpenFile(c.requestLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}

// redactBody returns body as JSON with credential values replaced. A body
// that is not JSON, such as an HTML error page, is logged as a truncated
// string so it can still be read.
func redactBody(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		s := string(body)
		if len(s) > 500 {
			s = s[:500] + "..."
		}
		out, _ := json.Marshal(s)
		return out
	}
	out, err := json.Marshal(redact(v))
	if err != nil {
		return nil
	}
	if len(out) > maxLoggedBody {
		out, _ = json.Marshal(string(out[:maxLoggedBody]) + "...(truncated)")
	}
	return out
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if redactedKeys[strings.ToLower(k)] && val != nil {
				t[k] = "[REDACTED]"
				continue
			}
			t[k] = redact(val)
		}
	case []any:
		for i := range t {
			t[i] = redact(t[i])
		}
	}
	return v
}
