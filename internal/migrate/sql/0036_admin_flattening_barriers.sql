-- No execution completion proof exists yet. Every stored confirmation is
-- pending, including zero inventory; later versioned work must refine this
-- predicate and keep missing progress closed rather than infer completion.
CREATE FUNCTION admin_flatten_account_pending(scope_account_id varchar) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (SELECT 1 FROM admin_account_flattenings WHERE account_id = scope_account_id AND delete_at IS NULL);
$$;

CREATE FUNCTION guard_admin_flatten_risk_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    scope_account_id varchar;
BEGIN
    IF NEW.enabled THEN
        SELECT account_id INTO scope_account_id FROM strategy_instances WHERE id = NEW.strategy_instance_id;
        PERFORM id FROM account_budgets WHERE account_id = scope_account_id FOR UPDATE;
        IF admin_flatten_account_pending(scope_account_id) THEN
            RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_risk_recovery', MESSAGE = 'account flattening is not resolved';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_flatten_risk_recovery_commit AFTER INSERT ON admin_strategy_risk_changes
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_flatten_risk_recovery();

CREATE FUNCTION guard_admin_flatten_strategy_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    previous_account_id varchar;
BEGIN
    previous_account_id := NEW.account_id;
    IF TG_OP = 'UPDATE' THEN
        IF ROW(NEW.account_id, NEW.instrument_id, NEW.template, NEW.code_version, NEW.requested_config_version, NEW.market_data_source,
               NEW.active_config_version, NEW.active_market_data_source, NEW.market_mode, NEW.delete_at)
           IS NOT DISTINCT FROM ROW(OLD.account_id, OLD.instrument_id, OLD.template, OLD.code_version, OLD.requested_config_version, OLD.market_data_source,
               OLD.active_config_version, OLD.active_market_data_source, OLD.market_mode, OLD.delete_at) THEN
            RETURN NULL;
        END IF;
        previous_account_id := OLD.account_id;
    END IF;
    -- Serialize affected account predicates with the existing persistent mutex.
    PERFORM id FROM account_budgets WHERE account_id IN (NEW.account_id, previous_account_id) ORDER BY account_id FOR UPDATE;
    IF admin_flatten_account_pending(NEW.account_id) OR admin_flatten_account_pending(previous_account_id) THEN
        RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_flatten_pending', MESSAGE = 'pending account flattening prevents binding or configuration changes';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_flatten_strategy_commit AFTER INSERT OR UPDATE ON strategy_instances
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_flatten_strategy_mutation();

CREATE FUNCTION guard_admin_flatten_configuration() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    scope_account_id varchar;
BEGIN
    SELECT account_id INTO scope_account_id FROM strategy_instances WHERE id = NEW.instance_id;
    PERFORM id FROM account_budgets WHERE account_id = scope_account_id FOR UPDATE;
    IF admin_flatten_account_pending(scope_account_id) THEN
        RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_flatten_pending', MESSAGE = 'pending account flattening prevents new configuration versions';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_flatten_configuration_commit AFTER INSERT ON strategy_config_versions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_flatten_configuration();

CREATE FUNCTION guard_admin_flatten_budget_grant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_budget strategy_budgets%ROWTYPE;
BEGIN
    SELECT * INTO current_budget FROM strategy_budgets WHERE id = NEW.id;
    IF current_budget.enabled AND current_budget.delete_at IS NULL THEN
        PERFORM id FROM account_budgets WHERE account_id = current_budget.account_id FOR UPDATE;
        IF admin_flatten_account_pending(current_budget.account_id) THEN
            RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_flatten_pending', MESSAGE = 'pending account flattening prevents a new risk budget grant';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_flatten_budget_commit AFTER INSERT OR UPDATE ON strategy_budgets
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_flatten_budget_grant();

CREATE FUNCTION guard_admin_flatten_cached_grant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_snapshot risk_snapshots%ROWTYPE;
BEGIN
    SELECT * INTO current_snapshot FROM risk_snapshots WHERE id = NEW.id;
    IF current_snapshot.allow_new_risk AND current_snapshot.delete_at IS NULL THEN
        -- Risk evaluation already holds this core mutex before reading global
        -- budget rows; do not introduce a late account-budget lock promotion.
        PERFORM id FROM simulator_accounts WHERE account_id = current_snapshot.account_id FOR UPDATE;
        IF admin_flatten_account_pending(current_snapshot.account_id) THEN
            RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_flatten_pending', MESSAGE = 'pending account flattening prevents a cached new risk grant';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_flatten_cached_grant_commit AFTER INSERT OR UPDATE ON risk_snapshots
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_flatten_cached_grant();
