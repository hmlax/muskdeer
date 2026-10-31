package adminopenapi

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/hmlax/muskdeer/internal/domain"
)

func manualSchemas() map[string]*Schema {
	return map[string]*Schema{
		"ErrorReply": object(map[string]*Schema{
			"code":    text(),
			"message": text(),
		}, "code", "message"),
		"LoginInput": object(map[string]*Schema{
			"username": text(),
			"password": text(),
		}, "username", "password"),
		"SessionReply": object(map[string]*Schema{
			"session": object(map[string]*Schema{
				"user_id":                   int64String(true),
				"username":                  text(),
				"role":                      stringsEnum(string(domain.AdminRoleViewer), string(domain.AdminRoleOperator), string(domain.AdminRoleAdmin)),
				"session_id":                int64String(true),
				"expires_at_ms":             int64String(true),
				"strategy_queries_enabled":  {Type: []string{"boolean"}},
				"strategy_controls_enabled": {Type: []string{"boolean"}},
			}, "user_id", "username", "role", "session_id", "expires_at_ms", "strategy_queries_enabled", "strategy_controls_enabled"),
		}, "session"),
		"ControlInput": object(map[string]*Schema{
			"action":            stringsEnum(string(domain.AdminControlStart), string(domain.AdminControlResume), string(domain.AdminControlPause), string(domain.AdminControlStop)),
			"expected_revision": int64String(true),
			"reason":            reason(),
		}, "action", "expected_revision", "reason"),
		"RiskInput": {
			OneOf: []*Schema{
				object(map[string]*Schema{
					"action":            stringsEnum(string(domain.AdminRiskBlock)),
					"expected_revision": int64String(true),
					"reason":            reason(),
				}, "action", "expected_revision", "reason"),
				object(map[string]*Schema{
					"action":                  stringsEnum(string(domain.AdminRiskUnblock)),
					"expected_revision":       int64String(true),
					"reason":                  reason(),
					"expected_config_version": int64String(true),
					"expected_desired_state":  stringsEnum(string(domain.StrategyStateRunning), string(domain.StrategyStatePaused), string(domain.StrategyStateStopped)),
				}, "action", "expected_revision", "reason", "expected_config_version", "expected_desired_state"),
			},
		},
	}
}

func reason() *Schema {
	return &Schema{
		Type:        []string{"string"},
		MinLength:   1,
		MaxLength:   256,
		Description: "Trimmed nonempty UTF-8 audit reason, at most 256 bytes and no NUL. Byte length and trimming are checked by the server.",
	}
}

func configureInput(name protoreflect.FullName, schema *Schema) {
	switch name {
	case "muskdeer.strategy.v1.CreateStrategyInput":
		schema.Required = []string{"instance_key", "account_id", "instrument_id", "code_version", "market_data_source", "market_mode", "parameters", "reason"}
		schema.Properties["instrument_id"] = int64String(true)
		schema.Properties["market_mode"] = marketModes()
	case "muskdeer.strategy.v1.PublishConfigurationInput":
		schema.Required = []string{"expected_revision", "expected_config_version", "market_data_source", "parameters", "reason"}
		schema.Properties["expected_revision"] = int64String(true)
		schema.Properties["expected_config_version"] = int64String(true)
	case "muskdeer.strategy.v1.OrderCancellationInput":
		schema.Required = []string{"expected_order_revision", "reason"}
		schema.Properties["expected_order_revision"] = int64String(true)
	case "muskdeer.common.v1.MakerParameters", "muskdeer.common.v1.GridParameters":
		schema.Description = "Typed template parameters. Financial values are plain decimal strings (amount/price/quantity at most 20 integer and 18 fractional digits; BPS at most 5 and 10). Durations are whole millisecond int64 strings bounded to one hour. Cross-field budgets, instrument increments, ranges, freshness and disabled hedge limits are checked by the server. Management creation/publication currently require hedging disabled."
		for field, property := range schema.Properties {
			if strings.HasSuffix(field, "_bps") {
				property.Pattern = `^[0-9]{1,5}(\.[0-9]{1,10})?$`
			} else if len(property.Type) == 1 && property.Type[0] == "string" && property.Format == "" {
				property.Pattern = `^[0-9]{1,20}(\.[0-9]{1,18})?$`
			}
		}
		if name == "muskdeer.common.v1.GridParameters" {
			schema.Required = []string{"lower_price", "upper_price", "grid_count", "total_budget", "per_order_notional", "inventory_limit", "max_market_age_ms"}
		} else {
			schema.Required = []string{"quote_levels", "level_spacing_bps", "base_spread_bps", "level_notional", "side_budget", "inventory_target", "inventory_soft_limit", "inventory_hard_limit", "reprice_bps", "min_refresh_interval_ms", "max_quote_age_ms", "max_market_age_ms", "hedge_threshold", "max_hedge_backlog"}
		}
	}
	if schema.Properties["market_data_source"] != nil {
		schema.Properties["market_data_source"] = marketSources()
	}
	if schema.Properties["reason"] != nil {
		schema.Properties["reason"] = reason()
	}
}
