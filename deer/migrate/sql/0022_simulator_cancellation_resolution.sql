ALTER TABLE cancellation_requests ADD CONSTRAINT ck_cancellation_core_identity CHECK (
    core_command_key = 'cancel:' || encode(sha256(convert_to(account_id || E'\n' || idempotency_key, 'UTF8')), 'hex')
);
CREATE INDEX idx_cancellation_core_key ON cancellation_requests(account_id, core_command_key);

CREATE FUNCTION validate_simulator_cancellation_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.error_code <> 'CORE_NOT_ACCEPTED' THEN
        RETURN NULL;
    END IF;
    IF current_setting('transaction_isolation') <> 'read committed' OR NEW.state <> 'REJECTED'
        OR NEW.deadline_at <= 0 OR NEW.deadline_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint THEN
        RAISE EXCEPTION 'cancellation resolution requires a closed deadline' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = NEW.strategy_instance_id FOR NO KEY UPDATE;
    PERFORM 1 FROM instruments WHERE id = NEW.instrument_id FOR NO KEY UPDATE;
    IF EXISTS (SELECT 1 FROM simulator_commands WHERE account_id = NEW.account_id AND action = 'CANCEL' AND command_key = NEW.core_command_key) THEN
        RAISE EXCEPTION 'accepted cancellation evidence is retained' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_cancellation_resolution AFTER INSERT OR UPDATE ON cancellation_requests
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_simulator_cancellation_resolution();

CREATE FUNCTION seal_simulator_cancellation_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    request cancellation_requests%ROWTYPE;
BEGIN
    IF NEW.action <> 'CANCEL' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO request FROM cancellation_requests WHERE account_id = NEW.account_id AND core_command_key = NEW.command_key;
    IF request.id IS NULL THEN
        RETURN NEW;
    END IF;
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'core admission requires current cancellation evidence' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = request.strategy_instance_id FOR NO KEY UPDATE;
    IF EXISTS (SELECT 1 FROM cancellation_requests WHERE account_id = NEW.account_id AND core_command_key = NEW.command_key AND error_code = 'CORE_NOT_ACCEPTED') THEN
        RAISE EXCEPTION 'resolved cancellation identity is sealed' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER simulator_cancellation_seal BEFORE INSERT ON simulator_commands
    FOR EACH ROW EXECUTE FUNCTION seal_simulator_cancellation_resolution();

CREATE FUNCTION retain_simulator_cancellation_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.error_code = 'CORE_NOT_ACCEPTED' THEN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'resolved cancellation history is retained' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
        END IF;
        IF NEW.state <> OLD.state OR NEW.error_code <> OLD.error_code OR NEW.core_command_key <> OLD.core_command_key
            OR NEW.delete_at IS DISTINCT FROM OLD.delete_at OR NEW.deadline_at <> OLD.deadline_at THEN
            RAISE EXCEPTION 'resolved cancellation cannot be reopened' USING ERRCODE = '23514', CONSTRAINT = 'simulator_cancellation_resolution';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER simulator_cancellation_history BEFORE UPDATE OR DELETE ON cancellation_requests
    FOR EACH ROW EXECUTE FUNCTION retain_simulator_cancellation_resolution();

-- Both kinds of L1 admission must re-read after their serialization mutex.
-- A repeatable snapshot cannot establish the absence of a committed seal.
CREATE OR REPLACE FUNCTION seal_simulator_submission_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    intent order_intents%ROWTYPE;
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'core admission requires a current transaction snapshot' USING ERRCODE = '23514', CONSTRAINT = 'simulator_submission_resolution';
    END IF;
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
