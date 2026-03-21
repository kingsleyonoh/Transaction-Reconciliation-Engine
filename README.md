# Transaction Reconciliation Engine — Automated payment matching across gateways, banks, and ledgers

Built by [Kingsley Onoh](https://kingsleyonoh.com) · Systems Architect

## The Problem

Any company processing payments through Stripe, PayPal, or bank transfers deals with the same headache: money moves through multiple systems, and the records never quite match. A refund shows up in the gateway but not the bank. A payment arrives three days late with a different reference number. A fee gets deducted that nobody expected. Finance teams spend hours in Excel every week reconciling these discrepancies — and for regulated businesses, auditors demand proof that every cent is accounted for. At scale (10,000+ transactions/day), manual reconciliation breaks down entirely.

## Architecture

```mermaid
%%{init: {'theme':'base','themeVariables':{'primaryColor':'#3B82F6','primaryTextColor':'#F0F0F5','primaryBorderColor':'#3B82F6','lineColor':'#3B82F6','secondaryColor':'#141418','tertiaryColor':'#0D0D0F','background':'#0D0D0F','mainBkg':'#141418','nodeBorder':'#3B82F6','clusterBkg':'#0D0D0F','clusterBorder':'#33333F','titleColor':'#F0F0F5','edgeLabelBackground':'#141418'}}}%%
graph TB
    A["Stripe API"] --> D["Ingester"]
    B["PayPal API"] --> D
    C["MT940 / CAMT.053\nBank Files"] --> D

    subgraph Engine ["Reconciliation Engine"]
        D --> E["Transaction Store\nPostgreSQL"]
        E --> F["4-Pass Matcher"]
        F --> G["Scorer\n4 Rules × Confidence"]
        G --> H["Discrepancy Manager"]
    end

    I["Redis"] --> D
    I --> F

    subgraph Output ["Reports & Actions"]
        H --> J["Discrepancy Tracker"]
        F --> K["Settlement Reports\nJSON + CSV"]
        H --> L["Auto-Resolution"]
    end

    M["Scheduler\nCron-based"] --> F
```

## Key Decisions

- **I chose Go over Python/Node** because reconciliation is CPU-bound comparison work. The 4-pass matching cascade evaluates every gateway transaction against every unmatched ledger entry — with 4,000 transactions, that's millions of comparisons. Go compiles to native code and handles this in 4.7 seconds where Python would take 30–60+.
- **I chose raw SQL (sqlx) over an ORM** because every query in a reconciliation engine is performance-critical. ORMs generate unpredictable queries, and when you're inserting 5,000 transactions in a batch or joining matches against discrepancies, you need to control the SQL exactly.
- **I chose integer cents over floating-point amounts** because `$19.99` as `1999` cents eliminates an entire class of rounding bugs. Financial software that uses `float64` for money is broken by design.
- **I chose a 4-rule scoring cascade over a single matching algorithm** because real-world transactions match in different ways. Exact matches (confidence 1.0) are obvious, but a bank entry arriving 2 days late needs fuzzy date matching (0.90), and a payment with a 0.3% fee difference needs fuzzy amount matching (0.75). Each rule produces a confidence score; the scorer picks the best.

## Setup

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- PostgreSQL 16 (provided via Docker)
- Redis 7 (provided via Docker)

### Installation

```bash
git clone https://github.com/kingsleyonoh/Transaction-Reconciliation-Engine.git
cd Transaction-Reconciliation-Engine
go mod download
```

### Environment

```bash
cp .env.example .env
```

| Variable | Purpose |
|----------|---------|
| `PORT` | HTTP server port (default: `8080`) |
| `API_KEY` | API authentication key |
| `DATABASE_URL` | PostgreSQL connection string |
| `REDIS_URL` | Redis connection string |
| `STRIPE_SECRET_KEY` | Stripe API key (`sk_test_` for sandbox) |
| `PAYPAL_CLIENT_ID` | PayPal OAuth client ID |
| `PAYPAL_CLIENT_SECRET` | PayPal OAuth client secret |
| `FUZZY_AMOUNT_TOLERANCE` | Amount match tolerance (default: `0.005` = 0.5%) |
| `MIN_CONFIDENCE_THRESHOLD` | Minimum score to accept a match (default: `0.70`) |
| `RECONCILE_SCHEDULE` | Cron expression for auto-reconciliation (default: `0 2 * * *`) |

### Run

```bash
# Docker (recommended)
docker compose up --build -d

# Apply migrations
make migrate-up

# Or run directly
go build -o recon ./cmd/recon && ./recon
```

## Usage

```bash
# Ingest transactions (single)
curl -X POST http://localhost:8080/api/v1/transactions/ingest \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id":"...","external_id":"txn_001","amount":9999,"currency":"USD","type":"credit","counterparty":"Acme Corp","occurred_at":"2026-03-15T10:00:00Z"}'

# Batch ingest (up to 1,000 per request)
curl -X POST http://localhost:8080/api/v1/transactions/ingest/batch \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"transactions":[...]}'

# Upload MT940 bank file
curl -X POST http://localhost:8080/api/v1/files/upload \
  -H "X-API-Key: $API_KEY" \
  -F "file=@statement.mt940" -F "source_id=..."

# Trigger reconciliation
curl -X POST http://localhost:8080/api/v1/reconcile \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"date_from":"2026-03-01T00:00:00Z","date_to":"2026-03-31T23:59:59Z","source_ids":["gateway-uuid","ledger-uuid"]}'

# Get discrepancies
curl http://localhost:8080/api/v1/discrepancies?status=open \
  -H "X-API-Key: $API_KEY"

# Generate settlement report (CSV)
curl http://localhost:8080/api/v1/reports/settlement?format=csv \
  -H "X-API-Key: $API_KEY"
```

## Tests

```bash
# Unit tests
go test ./internal/...

# PRD validation suite (requires Docker stack running)
docker compose up --build -d
go test -v -timeout 300s -run TestPRD ./tests/
```

The PRD validation suite runs 8 criteria against a live Docker stack with synthetic data:

| Criterion | What it tests | Threshold |
|-----------|---------------|-----------|
| Batch Ingest | 5,000 transactions + idempotency | < 30s |
| MT940 Parsing | Multi-record SWIFT bank file | 0 errors |
| Reconciliation | 4,000 txn matching performance | < 60s |
| Match Rate | Exact + fuzzy match accuracy | ≥ 85% |
| Fuzzy Matching | Date/amount tolerance detection | > 0 fuzzy matches |
| Discrepancies | Unmatched transaction flagging | > 0 records |
| Auto-Resolution | Discrepancy lifecycle on re-run | Status = resolved |
| Settlement CSV | Report generation | Valid CSV output |

<!-- THEATRE_LINK -->
