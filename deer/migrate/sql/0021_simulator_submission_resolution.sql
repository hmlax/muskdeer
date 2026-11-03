CREATE FUNCTION validate_simulator_submission_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.error_code <> 'CORE_NOT_ACCEPTED' THEN
        RETURN NULL;
    END IF;
    IF current_setting('transaction_isolation') <> 'read committed'
        OR NEW.state <> 'REJECTED' OR NEW.core_order_id IS NOT NULL OR NEW.core_revision <> 0
        OR NEW.filled_quantity <> 0 OR NEW.reserved_quote_notional <> 0
        OR NEW.submission_deadline_at <= 0
        OR NEW.submission_deadline_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint THEN
        RAISE EXCEPTION 'missing core resolution is not definitive' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = NEW.strategy_instance_id FOR NO KEY UPDATE;
    PERFORM 1 FROM instruments WHERE id = NEW.instrument_id FOR NO KEY UPDATE;
    IF EXISTS (SELECT 1 FROM simulator_orders WHERE account_id = NEW.account_id AND client_order_id = NEW.client_order_id)
        OR EXISTS (SELECT 1 FROM simulator_commands WHERE account_id = NEW.account_id AND action = 'SUBMIT'
            AND (client_order_id = NEW.client_order_id OR intent_id = NEW.id)) THEN
        RAISE EXCEPTION 'accepted core evidence cannot be resolved absent' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_submission_resolution AFTER INSERT OR UPDATE ON order_intents
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_simulator_submission_resolution();

CREATE FUNCTION seal_simulator_submission_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    intent order_intents%ROWTYPE;
BEGIN
    IF TG_TABLE_NAME = 'simulator_commands' THEN
        IF NEW.action <> 'SUBMIT' THEN
            RETURN NEW;
        END IF;
    END IF;
    SELECT * INTO intent FROM order_intents WHERE account_id = NEW.account_id AND client_order_id = NEW.client_order_id;
    IF intent.id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = intent.strategy_instance_id FOR NO KEY UPDATE;
    SELECT * INTO intent FROM order_intents WHERE id = intent.id;
    IF intent.error_code = 'CORE_NOT_ACCEPTED' THEN
        RAISE EXCEPTION 'resolved submission identity is sealed' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER simulator_submission_seal BEFORE INSERT ON simulator_orders
    FOR EACH ROW EXECUTE FUNCTION seal_simulator_submission_resolution();
CREATE TRIGGER simulator_submission_seal BEFORE INSERT ON simulator_commands
    FOR EACH ROW EXECUTE FUNCTION seal_simulator_submission_resolution();

CREATE FUNCTION retain_simulator_submission_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.error_code = 'CORE_NOT_ACCEPTED' THEN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'resolved submission history is retained' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
        END IF;
        IF NEW.state <> OLD.state OR NEW.error_code <> OLD.error_code
            OR NEW.delete_at IS DISTINCT FROM OLD.delete_at OR NEW.submission_deadline_at <> OLD.submission_deadline_at THEN
            RAISE EXCEPTION 'resolved submission cannot be reopened' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER simulator_submission_history BEFORE UPDATE OR DELETE ON order_intents
    FOR EACH ROW EXECUTE FUNCTION retain_simulator_submission_resolution();
