-- No execution completion proof exists yet: every accepted scope remains
-- pending. Later versioned progress/completion work must refine this predicate;
-- absence of that work cannot implicitly authorize recovery.
CREATE FUNCTION admin_emergency_scope_pending(scope_strategy_id bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM admin_emergency_cancellations
        WHERE strategy_instance_id = scope_strategy_id AND delete_at IS NULL
    );
$$;

CREATE FUNCTION guard_admin_emergency_risk_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.enabled AND admin_emergency_scope_pending(NEW.strategy_instance_id) THEN
        RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'admin_risk_recovery', MESSAGE = 'emergency scope is not resolved';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER admin_emergency_recovery_commit AFTER INSERT ON admin_strategy_risk_changes
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_admin_emergency_risk_recovery();
