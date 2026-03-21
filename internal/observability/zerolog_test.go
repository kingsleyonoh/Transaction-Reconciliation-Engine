package observability

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger_DefaultLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("info", &buf)

	logger.Info().Msg("hello")
	logger.Debug().Msg("should not appear")

	lines := splitLogLines(buf.Bytes())
	assert.Len(t, lines, 1, "debug should be filtered at info level")

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "hello", entry["message"])
}

func TestNewLogger_DebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("debug", &buf)

	logger.Debug().Msg("visible")

	lines := splitLogLines(buf.Bytes())
	assert.Len(t, lines, 1, "debug should be visible at debug level")

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	assert.Equal(t, "debug", entry["level"])
}

func TestRequestLoggerMiddleware_LogsFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("info", &buf)

	handler := RequestLoggerMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := splitLogLines(buf.Bytes())
	require.Len(t, lines, 1)

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/api/v1/health", entry["path"])
	assert.Equal(t, float64(200), entry["status"])
	assert.Contains(t, entry, "duration_ms")
}

func TestRequestLoggerMiddleware_StatusCode(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("info", &buf)

	handler := RequestLoggerMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := splitLogLines(buf.Bytes())
	require.Len(t, lines, 1)

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	assert.Equal(t, float64(500), entry["status"])
	assert.Equal(t, "error", entry["level"], "5xx should log at error level")
}

// splitLogLines splits newline-delimited JSON log output into individual lines.
func splitLogLines(data []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}
