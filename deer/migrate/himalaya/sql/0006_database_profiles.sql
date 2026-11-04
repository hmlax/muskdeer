-- Old rows deliberately remain unconfigured until an explicit reviewed import.
ALTER TABLE himalaya_budgets
  ADD COLUMN symbol varchar(32) NOT NULL DEFAULT '',
  ADD COLUMN market_data_source varchar(32) NOT NULL DEFAULT '',
  ADD COLUMN price_tick numeric(76,36) NOT NULL DEFAULT 0 CHECK (price_tick >= 0),
  ADD COLUMN quantity_step numeric(76,36) NOT NULL DEFAULT 0 CHECK (quantity_step >= 0);
CREATE UNIQUE INDEX uk_himalaya_budget_symbol ON himalaya_budgets(symbol)
  WHERE symbol <> '' AND delete_at IS NULL;
