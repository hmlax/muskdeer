-- Reuse the existing versioned maker_parameters JSONB; no core columns change.
ALTER TABLE strategy_config_versions DROP CONSTRAINT ck_strategy_parameters_shape;
ALTER TABLE strategy_config_versions ADD CONSTRAINT ck_strategy_parameters_shape CHECK (
    (parameters_schema_version = 0 AND template IS NULL AND maker_parameters IS NULL AND grid_parameters IS NULL)
    OR (parameters_schema_version = 1 AND template IS NOT NULL AND (
        (template = 'market_maker' AND maker_parameters IS NOT NULL AND jsonb_typeof(maker_parameters) = 'object' AND grid_parameters IS NULL AND NOT (maker_parameters ? 'venue_runtime'))
        OR (template = 'grid' AND grid_parameters IS NOT NULL AND jsonb_typeof(grid_parameters) = 'object' AND maker_parameters IS NULL)
    ))
    OR (parameters_schema_version = 2 AND template IS NOT DISTINCT FROM 'market_maker' AND grid_parameters IS NULL
        AND maker_parameters IS NOT NULL AND jsonb_typeof(maker_parameters) = 'object'
        AND maker_parameters ? 'venue_runtime'
        AND jsonb_typeof(maker_parameters -> 'venue_runtime') IS NOT DISTINCT FROM 'object'
        AND maker_parameters - ARRAY['venue_runtime'] = '{}'::jsonb)
);
ALTER TABLE strategy_config_versions DROP CONSTRAINT ck_maker_parameter_fields;
ALTER TABLE strategy_config_versions ADD CONSTRAINT ck_maker_parameter_fields CHECK (
    parameters_schema_version = 2 OR maker_parameters IS NULL OR maker_parameters - ARRAY[
        'quote_levels', 'level_spacing_bps', 'base_spread_bps', 'level_notional', 'side_budget',
        'inventory_target', 'inventory_soft_limit', 'inventory_hard_limit', 'reprice_bps',
        'min_refresh_interval_ms', 'max_quote_age_ms', 'max_market_age_ms', 'hedge_enabled',
        'hedge_threshold', 'hedge_max_wait_ms', 'max_hedge_backlog'
    ] = '{}'::jsonb
);
ALTER TABLE strategy_config_versions DROP CONSTRAINT ck_maker_decimal_strings;
ALTER TABLE strategy_config_versions ADD CONSTRAINT ck_maker_decimal_strings CHECK (

        parameters_schema_version = 2 OR maker_parameters IS NULL OR (
            jsonb_typeof(maker_parameters -> 'level_spacing_bps') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'base_spread_bps') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'level_notional') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'side_budget') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'inventory_target') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'inventory_soft_limit') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'inventory_hard_limit') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'reprice_bps') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'hedge_threshold') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(maker_parameters -> 'max_hedge_backlog') IS NOT DISTINCT FROM 'string'
        )

);

CREATE FUNCTION preserve_venue_strategy_parameters() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.parameters_schema_version = 2 THEN
            RAISE EXCEPTION 'venue strategy versions are immutable'
                USING ERRCODE = '23514', CONSTRAINT = 'venue_strategy_version_immutable';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.parameters_schema_version = 2 AND ROW(
        NEW.id, NEW.create_at, NEW.instance_id, NEW.version, NEW.market_data_source, NEW.code_version, NEW.created_by,
        NEW.parameters_schema_version, NEW.template, NEW.maker_parameters, NEW.delete_at
    ) IS DISTINCT FROM ROW(
        OLD.id, OLD.create_at, OLD.instance_id, OLD.version, OLD.market_data_source, OLD.code_version, OLD.created_by,
        OLD.parameters_schema_version, OLD.template, OLD.maker_parameters, OLD.delete_at
    ) THEN
        RAISE EXCEPTION 'venue strategy versions are immutable'
            USING ERRCODE = '23514', CONSTRAINT = 'venue_strategy_version_immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER venue_strategy_version_immutable BEFORE UPDATE OR DELETE ON strategy_config_versions
    FOR EACH ROW EXECUTE FUNCTION preserve_venue_strategy_parameters();
