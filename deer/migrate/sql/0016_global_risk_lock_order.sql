-- Preserve version 15 history while repairing its final-gate lock order.
CREATE OR REPLACE FUNCTION validate_simulator_global_risk() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    intent order_intents%ROWTYPE;
    budget risk_budgets%ROWTYPE;
    selected_asset varchar(32);
    count integer := 0;
BEGIN
    IF NEW.intent_id IS NULL OR NEW.action = 'CANCEL' THEN
        RETURN NEW;
    END IF;
    -- All final gates and reservation transitions lock the account/strategy
    -- budgets before intent/global rows. A concurrent release must not hold an
    -- account lock while waiting on a global lock acquired first by this gate.
    PERFORM 1 FROM account_budgets WHERE account_id = NEW.account_id FOR SHARE;
    PERFORM 1 FROM strategy_budgets WHERE strategy_instance_id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO intent FROM order_intents WHERE id = NEW.intent_id FOR SHARE;
    SELECT quote_asset INTO selected_asset FROM instruments WHERE id = NEW.instrument_id;
    FOR budget IN SELECT * FROM risk_budgets WHERE delete_at IS NULL AND asset = selected_asset
        AND ((scope_kind = 'PLATFORM' AND scope_key = selected_asset)
            OR (scope_kind = 'INSTRUMENT' AND instrument_id = NEW.instrument_id)) ORDER BY id FOR SHARE LOOP
        count := count + 1;
        IF NOT budget.enabled OR intent.price * intent.quantity > budget.maximum_order_notional
            OR budget.reserved_notional < intent.reserved_quote_notional THEN
            RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'global risk budget rejected', CONSTRAINT = 'oms_global_risk';
        END IF;
    END LOOP;
    IF count <> 2 THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'global risk budget is missing', CONSTRAINT = 'oms_global_risk';
    END IF;
    RETURN NEW;
END;
$$;
