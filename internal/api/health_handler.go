package api

import (
	"context"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

// healthStatus is the response body for the health endpoint.
type healthStatus struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

// HealthHandler returns an http.HandlerFunc that checks database and
// Redis connectivity, reporting individual and overall status.
func HealthHandler(db *sqlx.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		status := healthStatus{
			Status: "ok",
			DB:     "ok",
			Redis:  "ok",
		}

		// DB check: SELECT 1 with 2s timeout.
		if db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				status.DB = "error"
				status.Status = "degraded"
			}
		} else {
			status.DB = "not_configured"
		}

		// Redis check: PING with 1s timeout.
		if rdb != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				status.Redis = "error"
				status.Status = "degraded"
			}
		} else {
			status.Redis = "not_configured"
		}

		httpStatus := http.StatusOK
		if status.Status != "ok" {
			httpStatus = http.StatusServiceUnavailable
		}
		RespondJSON(w, httpStatus, status)
	}
}
