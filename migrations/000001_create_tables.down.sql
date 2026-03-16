-- 000001_create_tables.down.sql
-- Rollback: drop all tables in reverse dependency order

BEGIN;

DROP TRIGGER IF EXISTS trg_ingestion_logs_updated_at ON ingestion_logs;
DROP TRIGGER IF EXISTS trg_discrepancies_updated_at ON discrepancies;
DROP TRIGGER IF EXISTS trg_matches_updated_at ON matches;
DROP TRIGGER IF EXISTS trg_runs_updated_at ON reconciliation_runs;
DROP TRIGGER IF EXISTS trg_transactions_updated_at ON transactions;
DROP TRIGGER IF EXISTS trg_sources_updated_at ON sources;
DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS ingestion_logs;
DROP TABLE IF EXISTS discrepancies;
DROP TABLE IF EXISTS matches;
DROP TABLE IF EXISTS reconciliation_runs;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS sources;

COMMIT;
