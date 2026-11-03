-- One SQL statement observes base assets and all uncompleted buys consistently.
-- Sells never offset this worst case; a lost response is not proof of absence.
CREATE FUNCTION risk_inventory_allows(account_key varchar, instrument_key bigint) RETURNS boolean LANGUAGE sql AS $$
WITH policy AS (
    SELECT min(CASE
        WHEN c.parameters_schema_version = 1 AND c.template = 'market_maker'
            AND jsonb_typeof(c.maker_parameters->'inventory_hard_limit') = 'string'
            AND (c.maker_parameters->>'inventory_hard_limit') ~ '^[0-9]{1,20}([.][0-9]{1,18})?$'
            THEN (c.maker_parameters->>'inventory_hard_limit')::numeric
        WHEN c.parameters_schema_version = 1 AND c.template = 'grid'
            AND jsonb_typeof(c.grid_parameters->'inventory_limit') = 'string'
            AND (c.grid_parameters->>'inventory_limit') ~ '^[0-9]{1,20}([.][0-9]{1,18})?$'
            THEN (c.grid_parameters->>'inventory_limit')::numeric
        ELSE 0 END) AS hard_limit,
        count(*) AS policy_count
    FROM strategy_instances s JOIN strategy_config_versions c ON c.instance_id = s.id AND c.version = s.active_config_version
    WHERE s.account_id = account_key AND s.instrument_id = instrument_key AND s.delete_at IS NULL AND c.delete_at IS NULL
), inventory AS (
    SELECT b.available + b.frozen AS quantity FROM simulator_balances b JOIN instruments i ON i.id = instrument_key
    WHERE b.account_id = account_key AND b.asset = i.base_asset AND b.delete_at IS NULL
), buys AS (
    SELECT COALESCE(sum(quantity - filled_quantity), 0) AS quantity FROM order_intents
    WHERE account_id = account_key AND instrument_id = instrument_key AND side = 'BUY' AND delete_at IS NULL
        AND state IN ('INTENT', 'SUBMITTING', 'LIVE', 'PARTIALLY_FILLED', 'UNKNOWN')
)
SELECT COALESCE((SELECT policy.hard_limit > 0 AND policy.policy_count BETWEEN 1 AND 128
    AND inventory.quantity + buys.quantity <= policy.hard_limit FROM policy CROSS JOIN inventory CROSS JOIN buys), false);
$$;

-- Account budget is the shared policy mutex. Runnable configuration cannot be
-- replaced beneath another strategy's existing worst-case obligation.
CREATE FUNCTION validate_strategy_inventory_policy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_strategy strategy_instances%ROWTYPE;
    account_budget account_budgets%ROWTYPE;
BEGIN
    SELECT * INTO current_strategy FROM strategy_instances WHERE id = NEW.id;
    IF TG_OP = 'UPDATE' AND ROW(NEW.active_config_version, NEW.active_market_data_source, NEW.delete_at)
        IS NOT DISTINCT FROM ROW(OLD.active_config_version, OLD.active_market_data_source, OLD.delete_at) THEN
        RETURN NEW;
    END IF;
    SELECT * INTO account_budget FROM account_budgets WHERE account_id = current_strategy.account_id FOR UPDATE;
    IF current_strategy.delete_at IS NOT NULL OR current_strategy.active_config_version IS NULL THEN
        IF EXISTS (SELECT 1 FROM order_intents WHERE strategy_instance_id = current_strategy.id AND delete_at IS NULL
            AND state IN ('INTENT', 'SUBMITTING', 'LIVE', 'PARTIALLY_FILLED', 'UNKNOWN')) THEN
            RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'strategy inventory policy has outstanding obligations';
        END IF;
        RETURN NEW;
    END IF;
    -- Metadata-only/initial unfunded fixtures cannot trade and remain storable.
    IF account_budget.id IS NULL OR NOT EXISTS (SELECT 1 FROM simulator_accounts WHERE account_id = current_strategy.account_id) THEN
        RETURN NEW;
    END IF;
    IF NOT risk_inventory_allows(current_strategy.account_id, current_strategy.instrument_id) THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'active inventory policy is below worst exposure';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER strategy_inventory_policy AFTER INSERT OR UPDATE ON strategy_instances
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_inventory_policy();

CREATE FUNCTION keep_strategy_parameter_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.parameters_schema_version = 1 OR EXISTS (SELECT 1 FROM strategy_instances WHERE id = OLD.instance_id AND active_config_version = OLD.version)
        OR EXISTS (SELECT 1 FROM order_intents WHERE strategy_instance_id = OLD.instance_id AND config_version = OLD.version) THEN
        IF TG_OP = 'DELETE' OR ROW(NEW.id, NEW.create_at, NEW.instance_id, NEW.version, NEW.market_data_source, NEW.code_version,
            NEW.parameters_schema_version, NEW.template, NEW.maker_parameters, NEW.grid_parameters, NEW.delete_at)
            IS DISTINCT FROM ROW(OLD.id, OLD.create_at, OLD.instance_id, OLD.version, OLD.market_data_source, OLD.code_version,
            OLD.parameters_schema_version, OLD.template, OLD.maker_parameters, OLD.grid_parameters, OLD.delete_at) THEN
            RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'typed or referenced strategy parameter version is immutable';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_parameter_version BEFORE UPDATE OR DELETE ON strategy_config_versions
    FOR EACH ROW EXECUTE FUNCTION keep_strategy_parameter_version();

CREATE FUNCTION validate_simulator_inventory_risk() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    intent order_intents%ROWTYPE;
BEGIN
    IF NEW.intent_id IS NULL OR NEW.action = 'CANCEL' THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM account_budgets WHERE account_id = NEW.account_id FOR SHARE;
    PERFORM 1 FROM strategy_budgets WHERE strategy_instance_id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO intent FROM order_intents WHERE id = NEW.intent_id FOR SHARE;
    IF intent.side = 'BUY' AND NOT risk_inventory_allows(NEW.account_id, NEW.instrument_id) THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'worst inventory risk rejected', CONSTRAINT = 'simulator_inventory_risk';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_inventory_risk AFTER INSERT ON simulator_commands
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_simulator_inventory_risk();
