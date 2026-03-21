package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	Port     int
	APIKey   string
	LogLevel string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// Stripe
	StripeSecretKey  string
	StripeSyncInterval time.Duration

	// PayPal
	PayPalClientID     string
	PayPalClientSecret string
	PayPalBaseURL      string
	PayPalSyncInterval time.Duration

	// Adapter mode
	UseFixtures bool

	// Reconciliation
	ReconcileSchedule      string
	FuzzyAmountTolerance   float64
	FuzzyDateToleranceDays int
	MinConfidenceThreshold float64

	// Discrepancy
	HighSeverityThresholdCents     int64
	CriticalSeverityThresholdCents int64

	// Observability (optional)
	SentryDSN string
}

// Load reads configuration from environment variables with validation.
func Load() (*Config, error) {
	cfg := &Config{
		Port:     getEnvInt("PORT", 8080),
		APIKey:   getEnvRequired("API_KEY"),
		LogLevel: getEnvDefault("LOG_LEVEL", "info"),

		DatabaseURL: getEnvRequired("DATABASE_URL"),
		RedisURL:    getEnvDefault("REDIS_URL", "redis://localhost:6379/0"),

		StripeSecretKey:  os.Getenv("STRIPE_SECRET_KEY"),
		StripeSyncInterval: getEnvDuration("STRIPE_SYNC_INTERVAL", 6*time.Hour),

		PayPalClientID:     os.Getenv("PAYPAL_CLIENT_ID"),
		PayPalClientSecret: os.Getenv("PAYPAL_CLIENT_SECRET"),
		PayPalBaseURL:      getEnvDefault("PAYPAL_BASE_URL", "https://api-m.sandbox.paypal.com"),
		PayPalSyncInterval: getEnvDuration("PAYPAL_SYNC_INTERVAL", 6*time.Hour),

		UseFixtures: getEnvBool("USE_FIXTURES", false),

		ReconcileSchedule:      getEnvDefault("RECONCILE_SCHEDULE", "0 2 * * *"),
		FuzzyAmountTolerance:   getEnvFloat("FUZZY_AMOUNT_TOLERANCE", 0.005),
		FuzzyDateToleranceDays: getEnvInt("FUZZY_DATE_TOLERANCE_DAYS", 3),
		MinConfidenceThreshold: getEnvFloat("MIN_CONFIDENCE_THRESHOLD", 0.70),

		HighSeverityThresholdCents:     int64(getEnvInt("HIGH_SEVERITY_THRESHOLD_CENTS", 10000)),
		CriticalSeverityThresholdCents: int64(getEnvInt("CRITICAL_SEVERITY_THRESHOLD_CENTS", 100000)),

		SentryDSN: os.Getenv("SENTRY_DSN"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", c.Port)
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.APIKey == "" {
		return fmt.Errorf("API_KEY is required")
	}
	if c.FuzzyAmountTolerance < 0 || c.FuzzyAmountTolerance > 1 {
		return fmt.Errorf("FUZZY_AMOUNT_TOLERANCE must be between 0 and 1, got %f", c.FuzzyAmountTolerance)
	}
	if c.MinConfidenceThreshold < 0 || c.MinConfidenceThreshold > 1 {
		return fmt.Errorf("MIN_CONFIDENCE_THRESHOLD must be between 0 and 1, got %f", c.MinConfidenceThreshold)
	}
	return nil
}

// Helper functions for reading environment variables with defaults.

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvRequired(key string) string {
	return os.Getenv(key) // validation happens in validate()
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
