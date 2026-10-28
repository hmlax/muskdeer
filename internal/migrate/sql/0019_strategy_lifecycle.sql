ALTER TABLE strategy_instances ADD COLUMN runtime_owner_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE strategy_instances ADD COLUMN runtime_owner_epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE strategy_instances ADD CONSTRAINT ck_strategy_runtime_owner CHECK (
    (runtime_owner_epoch = 0 AND runtime_owner_id = '') OR (runtime_owner_epoch > 0 AND length(runtime_owner_id) > 0)
);

CREATE FUNCTION protect_strategy_runtime_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.runtime_owner_epoch > 0 AND (NEW.runtime_owner_epoch < OLD.runtime_owner_epoch
        OR (NEW.runtime_owner_epoch = OLD.runtime_owner_epoch AND NEW.runtime_owner_id <> OLD.runtime_owner_id)) THEN
        RAISE EXCEPTION 'strategy runtime incarnation cannot regress' USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_fence';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_runtime_identity BEFORE UPDATE ON strategy_instances
    FOR EACH ROW EXECUTE FUNCTION protect_strategy_runtime_identity();

CREATE FUNCTION strategy_has_obligations(strategy_key bigint) RETURNS boolean LANGUAGE sql AS $$
SELECT EXISTS (SELECT 1 FROM order_intents WHERE strategy_instance_id = strategy_key AND delete_at IS NULL
    AND state IN ('INTENT', 'SUBMITTING', 'LIVE', 'PARTIALLY_FILLED', 'UNKNOWN'))
    OR EXISTS (SELECT 1 FROM simulator_orders WHERE strategy_instance_id = strategy_key AND delete_at IS NULL
        AND state IN ('LIVE', 'PARTIALLY_FILLED'))
    OR EXISTS (SELECT 1 FROM strategy_actions WHERE strategy_instance_id = strategy_key AND delete_at IS NULL
        AND state IN ('PENDING', 'DISPATCHING', 'WAITING', 'UNKNOWN'));
$$;

CREATE FUNCTION validate_strategy_runtime_authority() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    lease strategy_leases%ROWTYPE;
    now_ms bigint;
BEGIN
    -- Epoch zero preserves existing metadata and explicit L1 fixtures. The
    -- actual lifecycle claims a positive incarnation before any run transition.
    IF NEW.runtime_owner_epoch = 0 THEN
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND ROW(NEW.state, NEW.active_config_version, NEW.active_market_data_source,
        NEW.runtime_owner_id, NEW.runtime_owner_epoch, NEW.delete_at)
        IS NOT DISTINCT FROM ROW(OLD.state, OLD.active_config_version, OLD.active_market_data_source,
        OLD.runtime_owner_id, OLD.runtime_owner_epoch, OLD.delete_at) THEN
        RETURN NULL;
    END IF;
    SELECT * INTO lease FROM strategy_leases WHERE strategy_instance_id = NEW.id FOR SHARE;
    now_ms := floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint;
    IF lease.id IS NULL OR lease.delete_at IS NOT NULL OR lease.owner_id <> NEW.runtime_owner_id
        OR lease.epoch <> NEW.runtime_owner_epoch OR lease.expires_at <= now_ms THEN
        RAISE EXCEPTION 'strategy runtime authority rejected' USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_fence';
    END IF;
    IF NEW.state = 'RUNNING' THEN
        IF TG_OP = 'INSERT' OR NOT NEW.desired_running OR NEW.active_config_version IS NULL OR NEW.active_market_data_source IS NULL THEN
            RAISE EXCEPTION 'strategy runtime requires recovered active input' USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_transition';
        END IF;
        IF (NEW.runtime_owner_id <> OLD.runtime_owner_id OR NEW.runtime_owner_epoch <> OLD.runtime_owner_epoch)
            OR (OLD.state NOT IN ('RECOVERING', 'STARTING', 'RUNNING'))
            OR (OLD.state = 'RUNNING' AND ROW(NEW.active_config_version, NEW.active_market_data_source)
                IS DISTINCT FROM ROW(OLD.active_config_version, OLD.active_market_data_source)) THEN
            RAISE EXCEPTION 'strategy runtime must recover before running' USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_transition';
        END IF;
    END IF;
    IF NEW.state IN ('STOPPED', 'PAUSED') OR (TG_OP = 'UPDATE' AND ROW(NEW.active_config_version, NEW.active_market_data_source)
        IS DISTINCT FROM ROW(OLD.active_config_version, OLD.active_market_data_source))
        OR (NEW.state = 'RUNNING' AND (TG_OP = 'INSERT' OR OLD.state <> 'RUNNING')) THEN
        IF strategy_has_obligations(NEW.id) THEN
            RAISE EXCEPTION 'strategy runtime has unresolved obligations' USING ERRCODE = '23514', CONSTRAINT = 'strategy_runtime_unresolved';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER strategy_runtime_fence AFTER INSERT OR UPDATE ON strategy_instances
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_runtime_authority();
