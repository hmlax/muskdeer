-- Stopped instances retain development-run identity as well.
CREATE UNIQUE INDEX strategy_instances_wash_run_identity
ON strategy_instances (instrument_id, (parameters->'wash_trade'->>'run_id'))
WHERE template = 'wash_trade' AND delete_at IS NULL;

-- Configuration identity and requested running state have separate versions.
CREATE OR REPLACE FUNCTION guard_instance_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.account_id IS DISTINCT FROM OLD.account_id OR NEW.instrument_id IS DISTINCT FROM OLD.instrument_id OR NEW.template IS DISTINCT FROM OLD.template THEN
  RAISE EXCEPTION 'instance financial and algorithm identity is immutable';
 END IF;
 IF (NEW.template_id IS DISTINCT FROM OLD.template_id OR NEW.template_revision IS DISTINCT FROM OLD.template_revision OR NEW.parameters IS DISTINCT FROM OLD.parameters OR NEW.implementation IS DISTINCT FROM OLD.implementation OR NEW.market_data_source IS DISTINCT FROM OLD.market_data_source OR NEW.tape_policy_digest IS DISTINCT FROM OLD.tape_policy_digest OR NEW.tape_parameters_digest IS DISTINCT FROM OLD.tape_parameters_digest) AND NEW.revision <> OLD.revision + 1 THEN
  RAISE EXCEPTION 'configuration edit requires next instance revision';
 END IF;
 IF NEW.parameters ? 'management_change' THEN
  RAISE EXCEPTION 'management audit belongs only in immutable history';
 END IF;
 IF NEW.desired_running IS DISTINCT FROM OLD.desired_running AND NEW.requested_config_version <> OLD.requested_config_version + 1 THEN
  RAISE EXCEPTION 'control edit requires next control version';
 END IF;
 RETURN NEW;
END $$;
ALTER TABLE strategy_instances ADD CONSTRAINT current_parameters_without_audit CHECK (NOT parameters ? 'management_change');
