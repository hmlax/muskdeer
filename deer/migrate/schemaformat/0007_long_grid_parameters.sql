-- Extend the current typed configuration contract without rewriting any row.
ALTER TABLE strategy_instances DROP CONSTRAINT current_instance_parameters;
ALTER TABLE strategy_instances ADD CONSTRAINT current_instance_parameters CHECK ((
 parameters->>'schema_version' = '1' AND jsonb_typeof(parameters->'execution') = 'object' AND
 ((template='market_maker' AND implementation='depth_maker_v1' AND jsonb_typeof(parameters->'market_maker')='object' AND NOT parameters ? 'grid' AND NOT parameters ? 'wash_trade' AND NOT parameters ? 'long_grid') OR
  (template='grid' AND implementation='price_grid_v1' AND jsonb_typeof(parameters->'grid')='object' AND NOT parameters ? 'market_maker' AND NOT parameters ? 'wash_trade' AND NOT parameters ? 'long_grid') OR
  (template='wash_trade' AND implementation='wash_trade_v1' AND jsonb_typeof(parameters->'wash_trade')='object' AND NOT parameters ? 'market_maker' AND NOT parameters ? 'grid' AND NOT parameters ? 'long_grid') OR
  (template='grid' AND implementation='long_grid_v1' AND jsonb_typeof(parameters->'long_grid')='object' AND NOT parameters ? 'market_maker' AND NOT parameters ? 'grid' AND NOT parameters ? 'wash_trade' AND
   jsonb_typeof(parameters->'long_grid'->'spacing_fraction')='string' AND
   jsonb_typeof(parameters->'long_grid'->'stop_loss_fraction')='string' AND
   jsonb_typeof(parameters->'long_grid'->'order_notional')='string' AND
   jsonb_typeof(parameters->'long_grid'->'maximum_position_notional')='string' AND
   parameters->'long_grid'->>'open_on_start'='true' AND parameters->'long_grid'->>'halt_after_stop_loss'='true'))
) IS TRUE);

-- Different funds may trade the same instrument. The account identity remains
-- unique, so maker and grid scopes are independent rather than overwriting one.
DROP INDEX uk_himalaya_budget_symbol;
CREATE INDEX idx_trading_policy_symbol ON trading_account_policies(symbol)
 WHERE symbol <> '' AND delete_at IS NULL;
