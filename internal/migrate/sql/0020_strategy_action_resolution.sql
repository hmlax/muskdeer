ALTER TABLE strategy_actions DROP CONSTRAINT strategy_actions_state_check;
ALTER TABLE strategy_actions ADD CONSTRAINT strategy_actions_state_check CHECK (
    state IN ('PENDING', 'DISPATCHING', 'WAITING', 'UNKNOWN', 'OBSERVED', 'DISCARDED', 'NOT_ACCEPTED')
);
ALTER TABLE strategy_actions DROP CONSTRAINT ck_strategy_action_deadline;
ALTER TABLE strategy_actions ADD CONSTRAINT ck_strategy_action_deadline CHECK (
    (state IN ('PENDING', 'DISCARDED') AND deadline_at = 0)
    OR (state IN ('DISPATCHING', 'WAITING', 'UNKNOWN', 'OBSERVED', 'NOT_ACCEPTED') AND deadline_at > 0)
);
ALTER TABLE strategy_actions ADD CONSTRAINT ck_strategy_action_not_accepted CHECK (
    state <> 'NOT_ACCEPTED' OR (cancellation_id IS NULL AND observed_order_state = '' AND observed_cancellation_state = ''
        AND (kind = 'CANCEL' OR intent_id IS NULL))
);
CREATE INDEX idx_strategy_action_core_target ON strategy_actions(account_id, kind, client_order_id);

CREATE OR REPLACE FUNCTION protect_strategy_action_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'strategy action history is retained' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_identity';
    END IF;
    IF ROW(NEW.id, NEW.create_at, NEW.delete_at, NEW.strategy_instance_id, NEW.decision_key, NEW.account_id,
        NEW.instrument_id, NEW.config_version, NEW.kind, NEW.side, NEW.price, NEW.quantity, NEW.time_in_force,
        NEW.reduce_only, NEW.idempotency_key, NEW.client_order_id)
        IS DISTINCT FROM ROW(OLD.id, OLD.create_at, OLD.delete_at, OLD.strategy_instance_id, OLD.decision_key, OLD.account_id,
        OLD.instrument_id, OLD.config_version, OLD.kind, OLD.side, OLD.price, OLD.quantity, OLD.time_in_force,
        OLD.reduce_only, OLD.idempotency_key, OLD.client_order_id)
        OR NEW.revision <> OLD.revision + 1
        OR (OLD.intent_id IS NOT NULL AND NEW.intent_id IS DISTINCT FROM OLD.intent_id)
        OR (OLD.cancellation_id IS NOT NULL AND NEW.cancellation_id IS DISTINCT FROM OLD.cancellation_id)
        OR (OLD.deadline_at > 0 AND NEW.deadline_at <> OLD.deadline_at)
        OR NOT ((OLD.state = 'PENDING' AND NEW.state IN ('DISPATCHING', 'DISCARDED'))
            OR (OLD.state IN ('DISPATCHING', 'WAITING', 'UNKNOWN') AND NEW.state IN ('WAITING', 'UNKNOWN', 'OBSERVED', 'NOT_ACCEPTED'))) THEN
        RAISE EXCEPTION 'strategy action identity or transition rejected' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_identity';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION validate_strategy_action_not_accepted() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    now_ms bigint;
    core_key text;
BEGIN
    IF NEW.state <> 'NOT_ACCEPTED' THEN
        RETURN NULL;
    END IF;
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'action resolution requires current admission evidence' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    -- Execution admission takes this exact mutex before accepting a request.
    -- Wait for in-flight admission; never infer absence from a stale snapshot.
    PERFORM 1 FROM strategy_instances WHERE id = NEW.strategy_instance_id FOR NO KEY UPDATE;
    now_ms := floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint;
    IF NEW.deadline_at <= 0 OR NEW.deadline_at > now_ms THEN
        RAISE EXCEPTION 'original action deadline is not closed' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    IF NEW.kind = 'SUBMIT' THEN
        IF EXISTS (SELECT 1 FROM order_intents WHERE account_id = NEW.account_id
                AND (idempotency_key = NEW.idempotency_key OR client_order_id = NEW.client_order_id))
            OR EXISTS (SELECT 1 FROM simulator_orders WHERE account_id = NEW.account_id AND client_order_id = NEW.client_order_id)
            OR EXISTS (SELECT 1 FROM simulator_commands WHERE account_id = NEW.account_id AND action = 'SUBMIT' AND client_order_id = NEW.client_order_id) THEN
            RAISE EXCEPTION 'action has accepted submission evidence' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
        END IF;
    ELSE
        core_key := 'cancel:' || encode(sha256(convert_to(NEW.account_id || E'\n' || NEW.idempotency_key, 'UTF8')), 'hex');
        IF EXISTS (SELECT 1 FROM cancellation_requests WHERE account_id = NEW.account_id AND idempotency_key = NEW.idempotency_key)
            OR EXISTS (SELECT 1 FROM simulator_commands WHERE account_id = NEW.account_id AND action = 'CANCEL' AND command_key = core_key) THEN
            RAISE EXCEPTION 'action has accepted cancellation evidence' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER strategy_action_not_accepted AFTER INSERT OR UPDATE ON strategy_actions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_action_not_accepted();

CREATE FUNCTION prevent_resolved_action_admission() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    action strategy_actions%ROWTYPE;
    kind_key text;
BEGIN
    IF TG_TABLE_NAME = 'order_intents' THEN
        kind_key := 'SUBMIT';
    ELSE
        kind_key := 'CANCEL';
    END IF;
    SELECT * INTO action FROM strategy_actions WHERE account_id = NEW.account_id AND idempotency_key = NEW.idempotency_key;
    IF action.id IS NULL THEN
        RETURN NEW;
    END IF;
    IF action.kind <> kind_key OR action.strategy_instance_id <> NEW.strategy_instance_id OR action.instrument_id <> NEW.instrument_id THEN
        RAISE EXCEPTION 'action scope cannot be rebound' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = action.strategy_instance_id FOR NO KEY UPDATE;
    -- Re-read after serialization, including a just-committed resolution.
    SELECT * INTO action FROM strategy_actions WHERE id = action.id;
    IF action.state IN ('PENDING', 'DISCARDED', 'NOT_ACCEPTED') OR action.deadline_at <= 0 THEN
        RAISE EXCEPTION 'resolved or unsent action cannot be admitted' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    IF kind_key = 'SUBMIT' THEN
        IF NEW.submission_deadline_at > action.deadline_at
            OR NEW.submission_deadline_at <= floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint THEN
            RAISE EXCEPTION 'action deadline cannot be renewed' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
        END IF;
    ELSIF NEW.deadline_at > action.deadline_at
        OR NEW.deadline_at <= floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint THEN
        RAISE EXCEPTION 'action deadline cannot be renewed' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_action_admission BEFORE INSERT ON order_intents
    FOR EACH ROW EXECUTE FUNCTION prevent_resolved_action_admission();
CREATE TRIGGER strategy_action_admission BEFORE INSERT ON cancellation_requests
    FOR EACH ROW EXECUTE FUNCTION prevent_resolved_action_admission();

CREATE FUNCTION prevent_resolved_action_core_effect() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    action strategy_actions%ROWTYPE;
BEGIN
    IF TG_TABLE_NAME = 'simulator_orders' THEN
        SELECT * INTO action FROM strategy_actions WHERE account_id = NEW.account_id AND kind = 'SUBMIT' AND client_order_id = NEW.client_order_id;
    ELSIF NEW.action = 'SUBMIT' THEN
        SELECT * INTO action FROM strategy_actions WHERE account_id = NEW.account_id AND kind = 'SUBMIT' AND client_order_id = NEW.client_order_id;
    ELSE
        SELECT * INTO action FROM strategy_actions WHERE account_id = NEW.account_id AND kind = 'CANCEL'
            AND client_order_id = NEW.client_order_id
            AND 'cancel:' || encode(sha256(convert_to(account_id || E'\n' || idempotency_key, 'UTF8')), 'hex') = NEW.command_key;
    END IF;
    IF action.id IS NULL THEN
        RETURN NEW;
    END IF;
    IF action.strategy_instance_id <> NEW.strategy_instance_id THEN
        RAISE EXCEPTION 'action scope cannot be rebound' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    PERFORM 1 FROM strategy_instances WHERE id = action.strategy_instance_id FOR NO KEY UPDATE;
    SELECT * INTO action FROM strategy_actions WHERE id = action.id;
    IF action.state = 'NOT_ACCEPTED' THEN
        RAISE EXCEPTION 'resolved action cannot create a late core effect' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    IF TG_TABLE_NAME = 'simulator_commands' THEN
        IF NEW.deadline_at > action.deadline_at OR action.deadline_at <= floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint THEN
            RAISE EXCEPTION 'original action deadline cannot be renewed at core' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_action_core_effect BEFORE INSERT ON simulator_orders
    FOR EACH ROW EXECUTE FUNCTION prevent_resolved_action_core_effect();
CREATE TRIGGER strategy_action_core_effect BEFORE INSERT ON simulator_commands
    FOR EACH ROW EXECUTE FUNCTION prevent_resolved_action_core_effect();

CREATE FUNCTION retain_strategy_action_acceptance_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF ROW(NEW.id, NEW.create_at, NEW.account_id, NEW.idempotency_key, NEW.strategy_instance_id, NEW.instrument_id, NEW.client_order_id)
            IS DISTINCT FROM ROW(OLD.id, OLD.create_at, OLD.account_id, OLD.idempotency_key, OLD.strategy_instance_id, OLD.instrument_id, OLD.client_order_id) THEN
            RAISE EXCEPTION 'accepted business identity is retained' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
        END IF;
        IF NEW.delete_at IS NOT DISTINCT FROM OLD.delete_at THEN
            RETURN NEW;
        END IF;
    END IF;
    IF EXISTS (SELECT 1 FROM strategy_actions WHERE account_id = OLD.account_id AND idempotency_key = OLD.idempotency_key) THEN
        RAISE EXCEPTION 'action acceptance history is retained' USING ERRCODE = '23514', CONSTRAINT = 'strategy_action_not_accepted';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_action_history BEFORE UPDATE OR DELETE ON order_intents
    FOR EACH ROW EXECUTE FUNCTION retain_strategy_action_acceptance_history();
CREATE TRIGGER strategy_action_history BEFORE UPDATE OR DELETE ON cancellation_requests
    FOR EACH ROW EXECUTE FUNCTION retain_strategy_action_acceptance_history();
