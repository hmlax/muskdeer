ALTER TABLE strategy_instances ADD COLUMN desired_state varchar(16) NOT NULL DEFAULT '';
UPDATE strategy_instances SET desired_state = CASE
    WHEN desired_running THEN 'RUNNING'
    WHEN state IN ('PAUSING', 'PAUSED') THEN 'PAUSED'
    ELSE 'STOPPED'
END;

-- Backfill queues existing deferred identity/runtime checks. Validate them
-- before subsequent ALTER TABLE; never disable triggers or skip their checks.
-- Every deferrable constraint in versions 1..23 is INITIALLY DEFERRED.
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE strategy_instances ADD CONSTRAINT ck_strategy_desired_target CHECK (
    desired_state IN ('RUNNING', 'PAUSED', 'STOPPED')
    AND desired_running = (desired_state = 'RUNNING')
);

CREATE FUNCTION normalize_strategy_desired_target() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Existing explicit bool-only fixture/operator writers retain start/stop
    -- semantics. New pause requests must name PAUSED independently of state.
    IF TG_OP = 'INSERT' AND NEW.desired_state = '' THEN
        NEW.desired_state := CASE WHEN NEW.desired_running THEN 'RUNNING'
            WHEN NEW.state IN ('PAUSING', 'PAUSED') THEN 'PAUSED' ELSE 'STOPPED' END;
    ELSIF TG_OP = 'UPDATE' AND NEW.desired_state IS NOT DISTINCT FROM OLD.desired_state
        AND NEW.desired_running IS DISTINCT FROM OLD.desired_running THEN
        NEW.desired_state := CASE WHEN NEW.desired_running THEN 'RUNNING' ELSE 'STOPPED' END;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_desired_target BEFORE INSERT OR UPDATE ON strategy_instances
    FOR EACH ROW EXECUTE FUNCTION normalize_strategy_desired_target();

CREATE FUNCTION validate_strategy_runtime_target() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target varchar(16);
BEGIN
    IF NEW.runtime_owner_epoch = 0 OR (TG_OP = 'UPDATE' AND NEW.state = OLD.state) THEN
        RETURN NULL;
    END IF;
    target := CASE
        WHEN NEW.state = 'RUNNING' THEN 'RUNNING'
        WHEN NEW.state IN ('PAUSING', 'PAUSED') THEN 'PAUSED'
        WHEN NEW.state IN ('STOPPING', 'STOPPED') THEN 'STOPPED'
        ELSE NULL END;
    IF target IS NOT NULL AND NEW.desired_state <> target THEN
        RAISE EXCEPTION 'strategy runtime differs from requested target'
            USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_transition';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER strategy_runtime_target AFTER INSERT OR UPDATE ON strategy_instances
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_runtime_target();
SET CONSTRAINTS ALL DEFERRED;
