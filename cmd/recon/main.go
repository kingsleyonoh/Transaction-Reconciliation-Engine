package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/adapter"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/api"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/config"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/engine"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/lock"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/observability"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/report"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/repository"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/scheduler"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "sync":
		runSync()
	case "upload":
		runUpload()
	case "reconcile":
		runReconcile()
	case "report":
		runReport()
	case "discrepancies":
		runDiscrepancies()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: recon <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  serve                          Start the HTTP server")
	fmt.Println("  sync stripe|paypal             Manually trigger sync")
	fmt.Println("  upload <file>                  Ingest a bank statement file")
	fmt.Println("  reconcile --from --to          Run reconciliation")
	fmt.Println("  report --type --from --to      Generate report")
	fmt.Println("  discrepancies --status         List discrepancies")
}

// ---------------------------------------------------------------------------
// serve — start HTTP server with full dependency injection
// ---------------------------------------------------------------------------

func runServe() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize structured logger
	logger := observability.NewLogger(cfg.LogLevel, nil)

	// Initialize Sentry (no-op if DSN empty)
	if err := observability.InitSentry(cfg.SentryDSN); err != nil {
		logger.Warn().Err(err).Msg("sentry initialization failed")
	} else if cfg.SentryDSN != "" {
		logger.Info().Msg("✓ Sentry initialized")
	}
	defer observability.Flush()

	// Connect database
	db, err := repository.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()

	// Connect Redis
	rdb, err := repository.NewRedisClient(cfg.RedisURL)
	if err != nil {
		logger.Warn().Err(err).Msg("redis unavailable, continuing without cache")
		rdb = nil
	}
	if rdb != nil {
		defer rdb.Close()
	}

	// Startup health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Fatal().Err(err).Msg("database health check failed")
	}
	logger.Info().Msg("✓ Database connected")

	if rdb != nil {
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Warn().Err(err).Msg("redis health check failed")
			rdb = nil
		} else {
			logger.Info().Msg("✓ Redis connected")
		}
	}

	// Build repos
	txRepo := repository.NewTransactionRepo(db)
	runRepo := repository.NewRunRepo(db)
	matchRepo := repository.NewMatchRepo(db)
	discRepo := repository.NewDiscrepancyRepo(db)
	_ = repository.NewSourceRepo(db)

	// Build services
	ingester := engine.NewIngester(txRepo, rdb, logger)

	rules := buildMatchRules(cfg)
	scorer := engine.NewScorer(rules, cfg.MinConfidenceThreshold)
	// Build distributed lock
	var locker engine.DistributedLocker
	if rdb != nil {
		locker = lock.NewRedisLock(rdb)
		logger.Info().Msg("✓ Redis distributed lock enabled")
	}

	reconciler := engine.NewReconciler(
		scorer,
		txRepo,   // gwFetcher
		txRepo,   // lgFetcher
		matchRepo,
		discRepo,
		runRepo,
		locker,
		cfg.HighSeverityThresholdCents,
		cfg.CriticalSeverityThresholdCents,
	)

	// Build report generator
	reportGen := report.NewGenerator(runRepo, discRepo)

	// Build adapters
	adapters := map[string]adapter.SourceAdapter{
		"stripe":   adapter.NewStripeAdapter(cfg.StripeSecretKey, "", nil),
		"paypal":   adapter.NewPayPalAdapter(cfg.PayPalClientID, cfg.PayPalClientSecret, cfg.PayPalBaseURL, nil),
		"bankfile": adapter.NewBankFileAdapter(),
	}

	// Wire router
	deps := api.Deps{
		DB:        db,
		Redis:     rdb,
		APIKey:    cfg.APIKey,
		Logger:    logger,
		Ingester:  ingester,
		Adapters:  adapters,
		Runner:    reconciler,
		RunRepo:   runRepo,
		DiscRepo:  discRepo,
		DiscUpd:   discRepo,
		ReportGen: reportGen,
	}
	router := api.NewRouter(deps)

	// HTTP server with graceful shutdown
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start scheduler
	sched := scheduler.New(logger)
	sched.Register("stripe_sync", cfg.StripeSyncInterval, func(ctx context.Context) error {
		logger.Info().Msg("scheduler: stripe sync placeholder — requires live credentials")
		return nil
	})
	sched.Register("paypal_sync", cfg.PayPalSyncInterval, func(ctx context.Context) error {
		logger.Info().Msg("scheduler: paypal sync placeholder — requires live credentials")
		return nil
	})
	sched.Register("auto_reconcile", 24*time.Hour, func(ctx context.Context) error {
		now := time.Now().UTC()
		from := now.Add(-48 * time.Hour)
		_, err := reconciler.Reconcile(ctx, domain.ReconcileRequest{
			DateFrom: from,
			DateTo:   now,
		})
		return err
	})
	sched.Register("discrepancy_aging", 24*time.Hour, func(ctx context.Context) error {
		logger.Info().Msg("scheduler: discrepancy aging — auto-escalating stale items")
		return nil
	})
	sched.Register("stale_lock_cleanup", 30*time.Minute, func(ctx context.Context) error {
		logger.Info().Msg("scheduler: stale lock cleanup — Redis TTL handles expiry")
		return nil
	})
	sched.Start(shutdownCtx)

	go func() {
		logger.Info().Int("port", cfg.Port).Msg("server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("server error")
		}
	}()

	<-shutdownCtx.Done()
	logger.Info().Msg("shutting down server...")

	// Stop scheduler
	sched.Stop()

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		logger.Fatal().Err(err).Msg("server shutdown error")
	}
	logger.Info().Msg("server stopped gracefully")
}

// ---------------------------------------------------------------------------
// sync — manually trigger a sync for a specific source
// ---------------------------------------------------------------------------

func runSync() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: recon sync <stripe|paypal>")
		os.Exit(1)
	}
	source := os.Args[2]

	switch source {
	case "stripe", "paypal":
		fmt.Printf("Sync for %s is not yet implemented (requires connector credentials).\n", source)
		fmt.Println("Configure STRIPE_SECRET_KEY or PAYPAL_CLIENT_ID/SECRET and use the API endpoint.")
	default:
		fmt.Fprintf(os.Stderr, "Unknown source: %s (expected stripe or paypal)\n", source)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// upload — ingest a bank statement file
// ---------------------------------------------------------------------------

func runUpload() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: recon upload <file>")
		os.Exit(1)
	}
	filePath := os.Args[2]

	// Check file exists
	info, err := os.Stat(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot access file: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := repository.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	logger := observability.NewLogger(cfg.LogLevel, nil)

	txRepo := repository.NewTransactionRepo(db)
	ingester := engine.NewIngester(txRepo, nil, logger)

	// Read entire file
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("failed to open file: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
	}

	// Parse with BankFileAdapter
	bankAdapter := adapter.NewBankFileAdapter()
	ctx := context.Background()
	result, err := bankAdapter.ParseFile(ctx, data, "bankfile")
	if err != nil {
		log.Fatalf("failed to parse file: %v", err)
	}

	// Ingest transactions
	inserted := 0
	for _, tx := range result.Transactions {
		res, err := ingester.Ingest(ctx, tx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: ingestion error: %v\n", err)
			continue
		}
		if res.Status == "inserted" {
			inserted++
		}
	}

	fmt.Printf("File: %s (%d bytes)\n", filePath, info.Size())
	fmt.Printf("Parsed: %d transactions\n", result.TotalRecords)
	fmt.Printf("Inserted: %d new records\n", inserted)
}

// ---------------------------------------------------------------------------
// reconcile — run reconciliation for a date range
// ---------------------------------------------------------------------------

func runReconcile() {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	from := fs.String("from", "", "Start date (YYYY-MM-DD)")
	to := fs.String("to", "", "End date (YYYY-MM-DD)")
	fs.Parse(os.Args[2:])

	if *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "Usage: recon reconcile --from YYYY-MM-DD --to YYYY-MM-DD")
		os.Exit(1)
	}

	dateFrom, err := time.Parse("2006-01-02", *from)
	if err != nil {
		log.Fatalf("invalid from date: %v", err)
	}
	dateTo, err := time.Parse("2006-01-02", *to)
	if err != nil {
		log.Fatalf("invalid to date: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := repository.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	txRepo := repository.NewTransactionRepo(db)
	runRepo := repository.NewRunRepo(db)
	matchRepo := repository.NewMatchRepo(db)
	discRepo := repository.NewDiscrepancyRepo(db)

	rules := buildMatchRules(cfg)
	scorer := engine.NewScorer(rules, cfg.MinConfidenceThreshold)
	reconciler := engine.NewReconciler(
		scorer, txRepo, txRepo, matchRepo, discRepo, runRepo, nil,
		cfg.HighSeverityThresholdCents, cfg.CriticalSeverityThresholdCents,
	)

	ctx := context.Background()
	result, err := reconciler.Reconcile(ctx, domain.ReconcileRequest{
		DateFrom: dateFrom,
		DateTo:   dateTo,
	})
	if err != nil {
		log.Fatalf("reconciliation failed: %v", err)
	}

	fmt.Printf("Reconciliation completed:\n")
	fmt.Printf("  Run ID:             %s\n", result.RunID)
	fmt.Printf("  Matched:            %d\n", result.Matched)
	fmt.Printf("  Unmatched Gateway:  %d\n", result.UnmatchedGateway)
	fmt.Printf("  Unmatched Ledger:   %d\n", result.UnmatchedLedger)
	fmt.Printf("  Duration:           %dms\n", result.DurationMs)
}

// ---------------------------------------------------------------------------
// report — generate a settlement or discrepancy report
// ---------------------------------------------------------------------------

func runReport() {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	reportType := fs.String("type", "settlement", "Report type: settlement or discrepancy")
	from := fs.String("from", "", "Start date (YYYY-MM-DD)")
	to := fs.String("to", "", "End date (YYYY-MM-DD)")
	format := fs.String("format", "json", "Output format: json or csv")
	fs.Parse(os.Args[2:])

	if *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "Usage: recon report --type <settlement|discrepancy> --from YYYY-MM-DD --to YYYY-MM-DD [--format json|csv]")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := repository.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	runRepo := repository.NewRunRepo(db)
	discRepo := repository.NewDiscrepancyRepo(db)
	gen := report.NewGenerator(runRepo, discRepo)

	ctx := context.Background()
	req := domain.ReportRequest{DateFrom: *from, DateTo: *to}

	switch *reportType {
	case "settlement":
		rpt, err := gen.GenerateSettlement(ctx, req)
		if err != nil {
			log.Fatalf("failed to generate settlement report: %v", err)
		}
		outputReport(rpt, *format)

	case "discrepancy":
		rpt, err := gen.GenerateDiscrepancy(ctx, req)
		if err != nil {
			log.Fatalf("failed to generate discrepancy report: %v", err)
		}
		outputReport(rpt, *format)

	default:
		fmt.Fprintf(os.Stderr, "Unknown report type: %s\n", *reportType)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// discrepancies — list discrepancies with optional status filter
// ---------------------------------------------------------------------------

func runDiscrepancies() {
	fs := flag.NewFlagSet("discrepancies", flag.ExitOnError)
	status := fs.String("status", "", "Filter by status: open, investigating, resolved, ignored")
	limit := fs.Int("limit", 25, "Max results")
	fs.Parse(os.Args[2:])

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := repository.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	discRepo := repository.NewDiscrepancyRepo(db)
	ctx := context.Background()

	filters := repository.DiscrepancyFilters{
		Status: *status,
		Limit:  *limit,
	}
	discs, err := discRepo.FindByFilters(ctx, filters)
	if err != nil {
		log.Fatalf("failed to list discrepancies: %v", err)
	}

	if len(discs) == 0 {
		fmt.Println("No discrepancies found.")
		return
	}

	fmt.Printf("%-36s  %-20s  %-15s  %-10s  %s\n", "ID", "Transaction", "Type", "Severity", "Status")
	fmt.Println(repeatChar('-', 100))
	for _, d := range discs {
		fmt.Printf("%-36s  %-20s  %-15s  %-10s  %s\n",
			d.ID, truncate(d.TransactionID, 20), d.DiscrepancyType, d.Severity, d.Status)
	}
	fmt.Printf("\nTotal: %d discrepancies\n", len(discs))
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// buildMatchRules creates the standard set of matching rules from config.
func buildMatchRules(cfg *config.Config) []engine.MatchRule {
	return []engine.MatchRule{
		engine.NewExactMatchRule(),
		engine.NewAmountDateMatchRule(cfg.FuzzyDateToleranceDays),
		engine.NewReferenceMatchRule(),
		engine.NewFuzzyAmountMatchRule(cfg.FuzzyAmountTolerance, cfg.FuzzyDateToleranceDays),
	}
}

func outputReport(rpt interface{}, format string) {
	if format == "csv" {
		fmt.Println("CSV output to stdout is not yet supported. Use the API endpoint with format=csv.")
		return
	}
	// JSON output
	data, err := json.MarshalIndent(rpt, "", "  ")
	if err != nil {
		fmt.Printf("%+v\n", rpt)
		return
	}
	fmt.Println(string(data))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func repeatChar(ch byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
