package observability

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// NewLogger creates a zerolog.Logger writing JSON to w at the given level.
// If w is nil it defaults to os.Stdout.
// Supported levels: debug, info, warn, error. Everything else defaults to info.
func NewLogger(level string, w io.Writer) zerolog.Logger {
	if w == nil {
		w = os.Stdout
	}
	lvl := parseLevel(level)
	return zerolog.New(w).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}

func parseLevel(s string) zerolog.Level {
	switch s {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.code = code
	sw.ResponseWriter.WriteHeader(code)
}

// RequestLoggerMiddleware returns middleware that logs every HTTP request as
// structured JSON using the provided zerolog logger.
func RequestLoggerMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}

			next.ServeHTTP(sw, r)

			dur := time.Since(start)
			evt := logger.Info()
			if sw.code >= 500 {
				evt = logger.Error()
			} else if sw.code >= 400 {
				evt = logger.Warn()
			}
			evt.
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", sw.code).
				Float64("duration_ms", float64(dur.Microseconds())/1000.0).
				Str("remote_addr", r.RemoteAddr).
				Msg("request")
		})
	}
}
