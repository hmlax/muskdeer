ALTER TABLE strategy_instances ADD CONSTRAINT uk_strategy_instance_template UNIQUE (id, template);
ALTER TABLE strategy_config_versions
    ADD COLUMN parameters_schema_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN template varchar(32) NULL,
    ADD COLUMN maker_parameters jsonb NULL,
    ADD COLUMN grid_parameters jsonb NULL,
    ADD CONSTRAINT fk_strategy_config_template FOREIGN KEY (instance_id, template)
        REFERENCES strategy_instances(id, template),
    ADD CONSTRAINT ck_strategy_parameters_shape CHECK (
        (parameters_schema_version = 0 AND template IS NULL AND maker_parameters IS NULL AND grid_parameters IS NULL)
        OR
        (parameters_schema_version = 1 AND template IS NOT NULL AND (
            (template = 'market_maker' AND maker_parameters IS NOT NULL AND jsonb_typeof(maker_parameters) = 'object' AND grid_parameters IS NULL)
            OR
            (template = 'grid' AND grid_parameters IS NOT NULL AND jsonb_typeof(grid_parameters) = 'object' AND maker_parameters IS NULL)
        ))
    ),
    ADD CONSTRAINT ck_maker_parameter_fields CHECK (
        maker_parameters IS NULL OR maker_parameters - ARRAY[
            'quote_levels', 'level_spacing_bps', 'base_spread_bps', 'level_notional', 'side_budget',
            'inventory_target', 'inventory_soft_limit', 'inventory_hard_limit', 'reprice_bps',
            'min_refresh_interval_ms', 'max_quote_age_ms', 'max_market_age_ms', 'hedge_enabled',
            'hedge_threshold', 'hedge_max_wait_ms', 'max_hedge_backlog'
        ] = '{}'::jsonb
    ),
    ADD CONSTRAINT ck_grid_parameter_fields CHECK (
        grid_parameters IS NULL OR grid_parameters - ARRAY[
            'lower_price', 'upper_price', 'grid_count', 'total_budget', 'per_order_notional',
            'inventory_limit', 'max_market_age_ms'
        ] = '{}'::jsonb
    ),
    ADD CONSTRAINT ck_maker_decimal_strings CHECK (
        maker_parameters IS NULL OR (
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
    ),
    ADD CONSTRAINT ck_grid_decimal_strings CHECK (
        grid_parameters IS NULL OR (
            jsonb_typeof(grid_parameters -> 'lower_price') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(grid_parameters -> 'upper_price') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(grid_parameters -> 'total_budget') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(grid_parameters -> 'per_order_notional') IS NOT DISTINCT FROM 'string' AND
            jsonb_typeof(grid_parameters -> 'inventory_limit') IS NOT DISTINCT FROM 'string'
        )
    );
