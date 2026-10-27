ALTER TABLE order_intents ADD COLUMN core_revision bigint NOT NULL DEFAULT 0 CHECK (core_revision >= 0);
ALTER TABLE simulator_commands ADD COLUMN intent_id bigint NULL REFERENCES order_intents(id);

-- Direct simulator scenarios have no intent_id. Every OMS command supplies it
-- and atomically verifies risk again in the actual target commit transaction.
CREATE FUNCTION validate_simulator_intent_risk() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    intent order_intents%ROWTYPE;
    account_budget account_budgets%ROWTYPE;
    strategy_budget strategy_budgets%ROWTYPE;
    snapshot risk_snapshots%ROWTYPE;
    strategy strategy_instances%ROWTYPE;
    now_ms bigint;
BEGIN
    IF NEW.intent_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT * INTO account_budget FROM account_budgets WHERE account_id = NEW.account_id FOR SHARE;
    SELECT * INTO strategy_budget FROM strategy_budgets WHERE strategy_instance_id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO intent FROM order_intents WHERE id = NEW.intent_id FOR SHARE;
    IF intent.id IS NULL OR intent.delete_at IS NOT NULL OR intent.account_id <> NEW.account_id
        OR intent.client_order_id <> NEW.client_order_id OR intent.strategy_instance_id <> NEW.strategy_instance_id OR intent.instrument_id <> NEW.instrument_id THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'simulator intent binding rejected', CONSTRAINT = 'simulator_intent_risk';
    END IF;
    IF NEW.action = 'CANCEL' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO snapshot FROM risk_snapshots WHERE strategy_instance_id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO strategy FROM strategy_instances WHERE id = NEW.strategy_instance_id FOR SHARE;
    now_ms := floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint;
    IF intent.state NOT IN ('SUBMITTING', 'UNKNOWN') OR intent.fingerprint <> NEW.fingerprint OR intent.config_version <> NEW.config_version
        OR intent.filled_quantity <> 0 OR intent.reserved_quote_notional <> intent.price * intent.quantity
        OR account_budget.id IS NULL OR account_budget.delete_at IS NOT NULL OR NOT account_budget.enabled
        OR strategy_budget.id IS NULL OR strategy_budget.delete_at IS NOT NULL OR NOT strategy_budget.enabled
        OR account_budget.reserved_notional < intent.reserved_quote_notional OR strategy_budget.reserved_notional < intent.reserved_quote_notional
        OR account_budget.asset IS DISTINCT FROM (SELECT quote_asset FROM instruments WHERE id = NEW.instrument_id)
        OR snapshot.id IS NULL OR snapshot.delete_at IS NOT NULL OR NOT snapshot.allow_new_risk OR NOT snapshot.reconciled
        OR snapshot.account_id <> NEW.account_id OR snapshot.instrument_id <> NEW.instrument_id OR snapshot.config_version <> NEW.config_version
        OR snapshot.market_data_source IS DISTINCT FROM strategy.active_market_data_source
        OR snapshot.observed_at > now_ms OR snapshot.expires_at <= now_ms OR now_ms - snapshot.observed_at > 5000
        OR NOT EXISTS (SELECT 1 FROM simulator_orders WHERE initial_command_id = NEW.id AND side = intent.side AND price = intent.price
            AND quantity = intent.quantity AND time_in_force = intent.time_in_force AND reduce_only = intent.reduce_only) THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'simulator intent risk rejected', CONSTRAINT = 'simulator_intent_risk';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_intent_risk AFTER INSERT ON simulator_commands
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_simulator_intent_risk();
