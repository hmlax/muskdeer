-- Existing active pairs and their independently configured limits are retained.
-- A real runtime replacing/first activating a pair must commit the exact typed
-- template limit and zero reservations in the same transaction. Deferred checks
-- allow both write orders, but an old writer cannot silently keep the old limit.
CREATE FUNCTION validate_strategy_activation_budget() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    strategy strategy_instances%ROWTYPE;
    config strategy_config_versions%ROWTYPE;
    budget strategy_budgets%ROWTYPE;
    expected_limit numeric;
BEGIN
    IF NEW.runtime_owner_epoch = 0 OR NEW.active_config_version IS NULL THEN
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND ROW(NEW.active_config_version, NEW.active_market_data_source)
        IS NOT DISTINCT FROM ROW(OLD.active_config_version, OLD.active_market_data_source) THEN
        RETURN NULL;
    END IF;
    SELECT * INTO strategy FROM strategy_instances WHERE id = NEW.id;
    SELECT * INTO config FROM strategy_config_versions WHERE instance_id = strategy.id
        AND version = strategy.active_config_version AND market_data_source = strategy.active_market_data_source;
    SELECT * INTO budget FROM strategy_budgets WHERE strategy_instance_id = strategy.id FOR SHARE;
    IF config.parameters_schema_version = 1 AND config.template = strategy.template THEN
        CASE config.template
            WHEN 'market_maker' THEN expected_limit := 2 * (config.maker_parameters->>'side_budget')::numeric;
            WHEN 'grid' THEN expected_limit := (config.grid_parameters->>'total_budget')::numeric;
            ELSE expected_limit := NULL;
        END CASE;
    END IF;
    IF budget.id IS NULL OR budget.delete_at IS NOT NULL OR budget.account_id <> strategy.account_id
        OR config.delete_at IS NOT NULL OR expected_limit IS NULL OR expected_limit <= 0
        OR budget.reserved_notional <> 0 OR budget.limit_notional <> expected_limit THEN
        RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'strategy_activation_budget', MESSAGE = 'strategy activation budget is not synchronized';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER strategy_activation_budget AFTER INSERT OR UPDATE ON strategy_instances
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_activation_budget();
