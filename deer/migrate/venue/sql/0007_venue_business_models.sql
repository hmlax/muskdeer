-- Preserve durable identities, reservations, unknown attempts and observations.
-- Explicit maintenance only; every service checks the complete version history.
ALTER TABLE IF EXISTS himalaya_schema_migrations RENAME TO venue_schema_migrations;
ALTER TABLE himalaya_budgets RENAME TO trading_account_policies;
ALTER TABLE himalaya_intents RENAME TO venue_order_intents;
ALTER TABLE himalaya_cancellations RENAME TO venue_cancellations;
ALTER TABLE himalaya_projections RENAME TO venue_account_snapshots;
ALTER TABLE himalaya_development_baselines RENAME TO execution_recovery_baselines;
