# Transaction Reconciliation Engine — Codebase Context

> Last updated: 2026-03-14
> Template synced: 2026-03-14

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26.1 |
| Framework | Chi router (go-chi/chi) |
| Database | PostgreSQL 16 |
| Cache | Redis 7 |
| ORM | None — raw SQL with sqlx |
| Migrations | golang-migrate/migrate |
| Hosting | Docker on DigitalOcean VPS (Traefik) |
| Test Runner | Go stdlib testing + testify |
| Build Tool | Makefile + `go build` |
| Logging | zerolog (JSON to stdout) |
| Error Tracking | Sentry (free tier) |
| Bank File Parsing | mmalcek/mt940 + jHetzer/go-camt |

## Project Structure

```
transaction-reconciliation-engine/
├── cmd/
│   └── recon/
│       └── main.go                  # Entry point (stub — wiring TODO)
├── internal/
│   ├── adapter/                     # (planned) Source-specific adapters
│   ├── api/                        # (planned) HTTP handlers
│   ├── engine/                     # (planned) Core reconciliation logic
│   ├── domain/                     # Domain types ✓
│   │   ├── transaction.go          # Transaction + IngestRequest/Result
│   │   ├── match.go                # Match entity
│   │   ├── discrepancy.go          # Discrepancy entity
│   │   ├── reconciliation.go       # ReconcileRequest/Result
│   │   ├── source.go               # Source + IngestionLog entities
│   │   └── report.go               # Report types
│   ├── repository/                 # (planned) Database access
│   ├── scheduler/                  # (planned) Background job scheduling
│   ├── report/                     # (planned) Report generation
│   └── config/                     # Configuration ✓
│       └── config.go               # Env var loading + validation
├── migrations/                     # SQL migration files ✓
│   ├── 000001_create_tables.up.sql
│   └── 000001_create_tables.down.sql
├── tests/fixtures/                 # (planned) Integration tests + fixtures
│   ├── stripe/
│   ├── paypal/
│   └── bankfiles/
├── docs/
│   ├── progress.md
│   └── transaction-reconciliation-engine_prd.md
├── .env                            # Local dev config (gitignored)
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
└── go.sum
```

> ✓ = implemented, (planned) = directory exists but no source files yet

## Key Modules

| Module | Purpose | Key Files |
|--------|---------|-----------|
| Domain | Pure types — no imports | `internal/domain/*.go` |
| Repository | PostgreSQL CRUD with sqlx | `internal/repository/*.go` |
| Engine | 4-pass matching cascade | `internal/engine/reconciler.go` |
| Adapter | Source-specific transformers (Stripe, PayPal, bank files) | `internal/adapter/*.go` |
| API | Chi HTTP handlers + middleware | `internal/api/*.go` |
| Report | Settlement/discrepancy report generation (JSON + CSV) | `internal/report/*.go` |
| Scheduler | Ticker-based background jobs | `internal/scheduler/scheduler.go` |
| Config | Env var loading + validation | `internal/config/config.go` |

## Database Schema

| Table | Purpose | Key Fields |
|-------|---------|-----------|
| sources | Payment gateway/bank/ledger definitions | name (UNIQUE), source_type, config (JSONB) |
| transactions | Canonical transaction records from all sources | source_id (FK), external_id, amount (BIGINT cents), currency, dedup_key (UNIQUE) |
| matches | 1:1 links between gateway and ledger transactions | gateway_tx_id (UNIQUE FK), ledger_tx_id (UNIQUE FK), match_type, confidence |
| discrepancies | Unmatched/mismatched transactions requiring attention | transaction_id (FK), discrepancy_type, severity, status |
| reconciliation_runs | Audit trail for each reconciliation execution | started_at, matched_count, discrepancy_count, match_rate, duration_ms |
| ingestion_logs | Audit trail for each data ingestion operation | source_id (FK), records_total/new/skipped/failed |

## External Integrations

| Service | Purpose | Auth Method |
|---------|---------|------------|
| Stripe API | Pull balance transactions | Secret Key (Bearer token) |
| PayPal API | Pull transaction history | OAuth 2.0 Client Credentials |
| Bank Files (MT940/CAMT.053) | Ingest uploaded bank statements | API key (same as other endpoints) |
| Sentry | Error tracking | DSN env var |
| BetterStack | Uptime monitoring | External — configured in BetterStack UI |

## Environment Variables

| Variable | Purpose | Source |
|----------|---------|--------|
| PORT | HTTP server port | `.env` |
| API_KEY | API authentication key | `.env` |
| DATABASE_URL | PostgreSQL connection string | `.env` |
| REDIS_URL | Redis connection string | `.env` |
| STRIPE_SECRET_KEY | Stripe API auth | `.env` |
| PAYPAL_CLIENT_ID | PayPal OAuth client ID | `.env` |
| PAYPAL_CLIENT_SECRET | PayPal OAuth client secret | `.env` |
| PAYPAL_BASE_URL | PayPal API base URL (sandbox/production) | `.env` |
| USE_FIXTURES | Toggle fixture mode for CI/tests | `.env` |
| FUZZY_AMOUNT_TOLERANCE | Matching tolerance (default 0.005) | `.env` |
| MIN_CONFIDENCE_THRESHOLD | Minimum match confidence (default 0.70) | `.env` |

## Commands

| Action | Command |
|--------|---------|
| Dev server | `make dev` (uses `air` for hot reload) |
| Run tests | `go test ./...` or `make test` |
| Lint/check | `go vet ./...` or `make vet` |
| Build | `go build -o recon ./cmd/recon` or `make build` |
| Migrate DB (up) | `make migrate-up` |
| Migrate DB (down) | `make migrate-down` |
| Docker dev | `docker compose up -d` |

## Key Patterns & Conventions

- File naming: snake_case Go files, one file per handler/repository
- Import hierarchy: domain → nothing, repository → domain, engine → domain+repository, api → all
- Error handling: Return errors up the call stack, never swallow. Use `fmt.Errorf` with `%w` wrapping.
- Money: Always BIGINT cents, never floats. CHAR(3) ISO 4217 currency codes.
- Deduplication: `sha256(source_id + external_id)` stored in `dedup_key`, checked in Redis first then PostgreSQL.
- Logging: zerolog JSON to stdout. Every operation includes request_id and source context.

## Shared Foundation (MUST READ before any implementation)

> These files define the project's shared patterns, configuration, and utilities.
> The AI MUST read these **in full** before writing ANY new code.

| Category | File(s) | What it establishes |
|----------|---------|-------------------|
| Domain types | `internal/domain/*.go` | All entity structs (Transaction, Match, Discrepancy, etc.) |
| DB connection | `internal/repository/db.go` | PostgreSQL connection pool with sqlx |
| Config loader | `internal/config/config.go` | Environment variable parsing and validation |
| Router setup | `internal/api/router.go` | Chi router with middleware stack |
| Adapter interface | `internal/adapter/adapter.go` | SourceAdapter interface all adapters implement |

## Deep References

> For detailed implementation patterns, read the source directly — don't embed here.

| Topic | Where to look |
|-------|--------------|
| Reconciliation matching rules | `internal/engine/rules.go` |
| Confidence scoring | `internal/engine/scorer.go` |
| Stripe integration | `internal/adapter/stripe.go` |
| PayPal integration | `internal/adapter/paypal.go` |
| Bank file parsing | `internal/adapter/bankfile.go` |
| Report generation | `internal/report/` |
| Background jobs | `internal/scheduler/scheduler.go` |
| SQL migrations | `migrations/` |
| Test patterns | `tests/` |
| Test fixtures | `tests/fixtures/` |
