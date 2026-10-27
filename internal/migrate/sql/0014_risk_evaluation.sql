-- A closed decision may have no publisher at all. Zero records that absence;
-- only an enabled grant requires real positive publisher provenance.
ALTER TABLE risk_snapshots DROP CONSTRAINT risk_snapshots_market_epoch_check;
ALTER TABLE risk_snapshots ADD CONSTRAINT risk_snapshots_market_epoch_check CHECK (market_epoch >= 0);
ALTER TABLE risk_snapshots DROP CONSTRAINT ck_risk_market_provenance;
ALTER TABLE risk_snapshots ADD CONSTRAINT ck_risk_market_provenance CHECK (
    NOT allow_new_risk OR (market_feed_id IS NOT NULL AND market_revision > 0 AND market_source_version > 0 AND market_epoch > 0)
);
ALTER TABLE risk_snapshots ADD COLUMN reason_code varchar(32) NOT NULL DEFAULT 'NOT_EVALUATED'
    CHECK (reason_code IN ('NOT_EVALUATED', 'READY', 'STRATEGY_STOPPED', 'INVALID_PARAMETERS', 'BUDGET_DISABLED', 'UNRECONCILED', 'MARKET_UNAVAILABLE'));
