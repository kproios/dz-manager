package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridgeCredentialsNeverAppearInErrorLogs(t *testing.T) {
	for _, path := range []string{
		"/ingest/v1/private-test-credential/events",
		"/ingest/v1/private-test-credential/commands/poll",
		"/ingest/v1/private-test-credential/commands/123/result",
		"/ingest/v1/private-test-credential",
	} {
		t.Run(path, func(t *testing.T) {
			var output bytes.Buffer
			s := &Server{Log: slog.New(slog.NewJSONHandler(&output, nil))}
			handler := s.logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
			if strings.Contains(output.String(), "private-test-credential") || !strings.Contains(output.String(), "[redacted]") {
				t.Fatal("bridge credential was not removed from request log")
			}
		})
	}
}
