package config

import (
	"testing"
	"time"
)

// clearEnv unsets all config-relevant env vars so tests start clean.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PORT", "API_KEY", "LOG_LEVEL",
		"DATABASE_URL", "REDIS_URL",
		"STRIPE_SECRET_KEY", "STRIPE_SYNC_INTERVAL",
		"PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "PAYPAL_BASE_URL", "PAYPAL_SYNC_INTERVAL",
		"USE_FIXTURES",
		"RECONCILE_SCHEDULE", "FUZZY_AMOUNT_TOLERANCE", "FUZZY_DATE_TOLERANCE_DAYS",
		"MIN_CONFIDENCE_THRESHOLD",
		"HIGH_SEVERITY_THRESHOLD_CENTS", "CRITICAL_SEVERITY_THRESHOLD_CENTS",
	} {
		t.Setenv(key, "")
	}
}

// setRequiredEnv sets the minimum required env vars for a valid config.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("API_KEY", "test-api-key-12345")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb?sslmode=disable")
}

func TestLoad_MissingAPIKey_ReturnsError(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb?sslmode=disable")
	// API_KEY is empty string from clearEnv

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing API_KEY, got nil")
	}
}

func TestLoad_MissingDatabaseURL_ReturnsError(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_KEY", "test-api-key")
	// DATABASE_URL is empty string from clearEnv

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL, got nil")
	}
}

func TestLoad_InvalidPort_Zero_ReturnsError(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("PORT", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for PORT=0, got nil")
	}
}

func TestLoad_InvalidPort_TooHigh_ReturnsError(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("PORT", "99999")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for PORT=99999, got nil")
	}
}

func TestLoad_InvalidPort_NonNumeric_UsesDefault(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("PORT", "abc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// getEnvInt falls back to default 8080 for non-numeric
	if cfg.Port != 8080 {
		t.Errorf("expected Port=8080 (default), got %d", cfg.Port)
	}
}

func TestLoad_InvalidFuzzyAmountTolerance_Negative(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("FUZZY_AMOUNT_TOLERANCE", "-0.1")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative FUZZY_AMOUNT_TOLERANCE, got nil")
	}
}

func TestLoad_InvalidFuzzyAmountTolerance_Above1(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("FUZZY_AMOUNT_TOLERANCE", "1.5")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for FUZZY_AMOUNT_TOLERANCE > 1, got nil")
	}
}

func TestLoad_InvalidMinConfidenceThreshold_Negative(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("MIN_CONFIDENCE_THRESHOLD", "-0.5")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative MIN_CONFIDENCE_THRESHOLD, got nil")
	}
}

func TestLoad_InvalidMinConfidenceThreshold_Above1(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("MIN_CONFIDENCE_THRESHOLD", "2.0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MIN_CONFIDENCE_THRESHOLD > 1, got nil")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected Port=8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel=info, got %s", cfg.LogLevel)
	}
	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("expected default RedisURL, got %s", cfg.RedisURL)
	}
	if cfg.PayPalBaseURL != "https://api-m.sandbox.paypal.com" {
		t.Errorf("expected default PayPalBaseURL, got %s", cfg.PayPalBaseURL)
	}
	if cfg.ReconcileSchedule != "0 2 * * *" {
		t.Errorf("expected default ReconcileSchedule, got %s", cfg.ReconcileSchedule)
	}
	if cfg.FuzzyAmountTolerance != 0.005 {
		t.Errorf("expected FuzzyAmountTolerance=0.005, got %f", cfg.FuzzyAmountTolerance)
	}
	if cfg.FuzzyDateToleranceDays != 3 {
		t.Errorf("expected FuzzyDateToleranceDays=3, got %d", cfg.FuzzyDateToleranceDays)
	}
	if cfg.MinConfidenceThreshold != 0.70 {
		t.Errorf("expected MinConfidenceThreshold=0.70, got %f", cfg.MinConfidenceThreshold)
	}
	if cfg.StripeSyncInterval != 6*time.Hour {
		t.Errorf("expected StripeSyncInterval=6h, got %v", cfg.StripeSyncInterval)
	}
	if cfg.PayPalSyncInterval != 6*time.Hour {
		t.Errorf("expected PayPalSyncInterval=6h, got %v", cfg.PayPalSyncInterval)
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("STRIPE_SYNC_INTERVAL", "2h")
	t.Setenv("PAYPAL_SYNC_INTERVAL", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.StripeSyncInterval != 2*time.Hour {
		t.Errorf("expected StripeSyncInterval=2h, got %v", cfg.StripeSyncInterval)
	}
	if cfg.PayPalSyncInterval != 30*time.Minute {
		t.Errorf("expected PayPalSyncInterval=30m, got %v", cfg.PayPalSyncInterval)
	}
}

func TestLoad_CustomPortAndLogLevel(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("PORT", "3000")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("expected Port=3000, got %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel=debug, got %s", cfg.LogLevel)
	}
}

func TestLoad_UseFixturesTrue(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t)
	t.Setenv("USE_FIXTURES", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.UseFixtures {
		t.Error("expected UseFixtures=true, got false")
	}
}
