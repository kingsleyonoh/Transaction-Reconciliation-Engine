-- 000001_create_tables.up.sql
-- Creates all tables from PRD Section 4 (Data Model)

BEGIN;

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Sources table (PRD Section 4.1)
CREATE TABLE sources (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(100) NOT NULL UNIQUE,
    source_type VARCHAR(20)  NOT NULL CHECK (source_type IN ('gateway', 'bank', 'ledger')),
    config      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Transactions table (PRD Section 4.2)
CREATE TABLE transactions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source_id    UUID         NOT NULL REFERENCES sources(id),
    external_id  VARCHAR(255) NOT NULL,
    amount       BIGINT       NOT NULL,  -- stored in cents
    currency     VARCHAR(3)   NOT NULL DEFAULT 'EUR',
    direction    VARCHAR(10)  NOT NULL CHECK (direction IN ('credit', 'debit')),
    description  TEXT         NOT NULL DEFAULT '',
    counterparty VARCHAR(255) NOT NULL DEFAULT '',
    occurred_at  TIMESTAMPTZ  NOT NULL,
    raw_data     JSONB        NOT NULL DEFAULT '{}',
    dedup_key    VARCHAR(255) NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_transactions_dedup_key UNIQUE (dedup_key)
);

CREATE INDEX idx_transactions_source_id ON transactions(source_id);
CREATE INDEX idx_transactions_occurred_at ON transactions(occurred_at);
CREATE INDEX idx_transactions_amount_currency ON transactions(amount, currency);
CREATE INDEX idx_transactions_source_occurred ON transactions(source_id, occurred_at);

-- Reconciliation Runs table (PRD Section 4.3)
CREATE TABLE reconciliation_runs (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ,
    status            VARCHAR(20) NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    total_gateway     INT         NOT NULL DEFAULT 0,
    total_ledger      INT         NOT NULL DEFAULT 0,
    matched_count     INT         NOT NULL DEFAULT 0,
    discrepancy_count INT         NOT NULL DEFAULT 0,
    match_rate        NUMERIC(5,4) NOT NULL DEFAULT 0.0,
    config_snapshot   JSONB       NOT NULL DEFAULT '{}',
    error_message     TEXT        NOT NULL DEFAULT '',
    duration_ms       INT         NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_runs_status ON reconciliation_runs(status);
CREATE INDEX idx_runs_started_at ON reconciliation_runs(started_at);

-- Matches table (PRD Section 4.4)
CREATE TABLE matches (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    gateway_tx_id UUID NOT NULL REFERENCES transactions(id),
    ledger_tx_id  UUID NOT NULL REFERENCES transactions(id),
    match_type   VARCHAR(20)  NOT NULL CHECK (match_type IN ('exact', 'fuzzy_amount', 'fuzzy_date', 'reference', 'manual')),
    confidence   NUMERIC(5,4) NOT NULL DEFAULT 1.0,
    matched_by   VARCHAR(20)  NOT NULL CHECK (matched_by IN ('auto_exact', 'auto_fuzzy', 'manual_user')),
    match_rule   TEXT         NOT NULL DEFAULT '',
    run_id       UUID         NOT NULL REFERENCES reconciliation_runs(id),
    notes        TEXT         NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_matches_pair UNIQUE (gateway_tx_id, ledger_tx_id)
);

CREATE INDEX idx_matches_run_id ON matches(run_id);
CREATE INDEX idx_matches_gateway_tx_id ON matches(gateway_tx_id);
CREATE INDEX idx_matches_ledger_tx_id ON matches(ledger_tx_id);

-- Discrepancies table (PRD Section 4.5)
CREATE TABLE discrepancies (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id   UUID         NOT NULL REFERENCES transactions(id),
    discrepancy_type VARCHAR(30)  NOT NULL CHECK (discrepancy_type IN ('unmatched_gateway', 'unmatched_ledger', 'amount_mismatch', 'date_mismatch')),
    expected_value   TEXT         NOT NULL DEFAULT '',
    actual_value     TEXT         NOT NULL DEFAULT '',
    severity         VARCHAR(10)  NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    status           VARCHAR(20)  NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'resolved', 'ignored')),
    resolved_at      TIMESTAMPTZ,
    resolved_by      VARCHAR(100) NOT NULL DEFAULT '',
    resolution_note  TEXT         NOT NULL DEFAULT '',
    run_id           UUID         NOT NULL REFERENCES reconciliation_runs(id),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_discrepancies_status ON discrepancies(status);
CREATE INDEX idx_discrepancies_severity ON discrepancies(severity);
CREATE INDEX idx_discrepancies_run_id ON discrepancies(run_id);
CREATE INDEX idx_discrepancies_transaction_id ON discrepancies(transaction_id);

-- Ingestion Logs table (PRD Section 4.6)
CREATE TABLE ingestion_logs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source_id       UUID         NOT NULL REFERENCES sources(id),
    file_name       VARCHAR(500) NOT NULL DEFAULT '',
    records_total   INT          NOT NULL DEFAULT 0,
    records_new     INT          NOT NULL DEFAULT 0,
    records_skipped INT          NOT NULL DEFAULT 0,
    records_failed  INT          NOT NULL DEFAULT 0,
    errors          JSONB        NOT NULL DEFAULT '[]',
    started_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    status          VARCHAR(20)  NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ingestion_logs_source_id ON ingestion_logs(source_id);
CREATE INDEX idx_ingestion_logs_started_at ON ingestion_logs(started_at);

-- Trigger: auto-update updated_at on any row modification
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_sources_updated_at BEFORE UPDATE ON sources FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_transactions_updated_at BEFORE UPDATE ON transactions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_runs_updated_at BEFORE UPDATE ON reconciliation_runs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_matches_updated_at BEFORE UPDATE ON matches FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_discrepancies_updated_at BEFORE UPDATE ON discrepancies FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_ingestion_logs_updated_at BEFORE UPDATE ON ingestion_logs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMIT;
