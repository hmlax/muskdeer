package adminopenapi

import (
	"strings"

	"google.golang.org/protobuf/proto"

	adminv1 "github.com/hmlax/muskdeer/api/admin/v1"
	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const (
	featureAuth     = "admin_http.enabled"
	featureQueries  = "admin_http.strategy_queries_enabled"
	featureControls = "admin_http.strategy_controls_enabled"
	jsonMediaType   = "application/json"
)

type definition struct {
	path        string
	id          string
	reply       proto.Message
	input       proto.Message
	write       bool
	feature     string
	parameters  []Parameter
	replyName   string
	bodyName    string
	status      string
	description string
}

func definitions() []definition {
	strategyPath := "/api/v1/strategies/{strategyID}"
	result := []definition{
		{
			path:      "/api/v1/auth/login",
			id:        "login",
			write:     true,
			feature:   featureAuth,
			replyName: "SessionReply",
			bodyName:  "LoginInput",
			status:    "200",
		},
		{
			path:      "/api/v1/auth/session",
			id:        "session",
			feature:   featureAuth,
			replyName: "SessionReply",
			status:    "200",
		},
		{
			path:    "/api/v1/auth/logout",
			id:      "logout",
			write:   true,
			feature: featureAuth,
			status:  "204",
		},
		query("/api/v1/strategies", "listStrategies", &strategyv1.ListStrategiesReply{}, page()...),
		query(strategyPath, "getStrategy", &strategyv1.GetStrategyReply{}),
		query(strategyPath+"/versions", "listConfigVersions", &strategyv1.ListConfigVersionsReply{}, page()...),
		query("/api/v1/operations/{operationID}", "getOperation", &strategyv1.StrategyOperation{}),
		query("/api/v1/accounts", "listAccounts", &strategyv1.ListAccountsReply{}, page()...),
		query("/api/v1/instruments", "listInstruments", &strategyv1.ListInstrumentsReply{}, page()...),
		query("/api/v1/instruments/{instrumentID}/market-feeds", "listMarketFeeds", &strategyv1.ListMarketFeedsReply{}, paged(parameter("mode", true, marketModes()))...),
		query("/api/v1/audits", "listAudits", &strategyv1.ListAuditsReply{}, paged(parameter("actor", false, text()), parameter("action", false, text()), parameter("resource", false, text()))...),
		query("/api/v1/audits/{auditID}", "getAudit", &strategyv1.GetAuditReply{}),
		query(strategyPath+"/orders", "listStrategyOrders", &strategyv1.ListStrategyOrdersReply{}, paged(parameter("state", false, orderStates()), parameter("open_only", false, stringsEnum("true", "false")))...),
		query(strategyPath+"/orders/{orderID}", "getStrategyOrder", &strategyv1.GetStrategyOrderReply{}),
		query(strategyPath+"/trades", "listStrategyTrades", &strategyv1.ListStrategyTradesReply{}, paged(parameter("side", false, stringsEnum(string(domain.SideBuy), string(domain.SideSell))), parameter("order_id", false, int64String(true)))...),
		query(strategyPath+"/trades/{tradeID}", "getStrategyTrade", &strategyv1.GetStrategyTradeReply{}),
		query("/api/v1/portfolios", "listPortfolios", &strategyv1.ListPortfoliosReply{}, paged(parameter("account_id", false, text()), parameter("instrument_id", false, int64String(true)))...),
		query("/api/v1/portfolios/{portfolioID}", "getPortfolio", &strategyv1.GetPortfolioReply{}),
		query("/api/v1/risk-events", "listRiskEvents", &strategyv1.ListRiskEventsReply{}, riskEventParameters()...),
		query("/api/v1/risk-events/{eventID}", "getRiskEvent", &strategyv1.GetRiskEventReply{}),
		query("/api/v1/markets", "listMarkets", &strategyv1.ListMarketsReply{}, paged(parameter("mode", true, marketModes()), parameter("source", false, marketSources()), parameter("instrument_id", false, int64String(true)))...),
		query("/api/v1/markets/{feedID}", "getMarket", &strategyv1.GetMarketReply{}),
		query("/api/v1/overview/strategies", "getStrategyOverview", &strategyv1.GetStrategyOverviewReply{}, paged(parameter("account_id", false, text()), parameter("requested_market_data_source", false, marketSources()))...),
		query("/api/v1/overview/services", "getServiceOverview", &adminv1.ServiceOverview{}, serviceParameters()...),
		query(strategyPath+"/risk", "getStrategyRisk", &strategyv1.StrategyRiskState{}),
		query(strategyPath+"/order-cancellations/{cancellationID}", "getOrderCancellation", &strategyv1.OrderCancellationReceipt{}),
		mutation("/api/v1/strategies", "createStrategy", &strategyv1.CreationReceipt{}, &strategyv1.CreateStrategyInput{}, "201", "Creates immutable metadata and initial parameters; never starts trading or initializes funds."),
		mutation(strategyPath+"/configurations", "publishConfiguration", &strategyv1.PublicationReceipt{}, &strategyv1.PublishConfigurationInput{}, "201", "Publishes a requested configuration; does not assert activation, market-source fallback or resumed trading."),
		mutation(strategyPath+"/risk", "changeStrategyRisk", &strategyv1.StrategyRiskReceipt{}, nil, "200", "BLOCK requires operator; UNBLOCK requires administrator and both expected configuration and desired state. A receipt is historical, never a current risk grant. Recovery denial returns 409 "+domain.AdminRiskRecoveryDeniedCode+"."),
		mutation(strategyPath+"/orders/{orderID}/cancellations", "requestOrderCancellation", &strategyv1.OrderCancellationReceipt{}, &strategyv1.OrderCancellationInput{}, "202", "202 means accepted cancellation work; 200 is an already observed terminal result. UNKNOWN retains funds/exposure reservations; terminal FILLED does not mean canceled."),
		mutation(strategyPath+"/control", "controlStrategy", &strategyv1.StrategyOperation{}, nil, "202", "202 means accepted desired-state intent; 200 is a previously observed result. Stop waits for terminal orders; stop, cancellation and flattening are separate operations. No flatten endpoint is provided."),
	}
	return result
}

func query(path, id string, reply proto.Message, parameters ...Parameter) definition {
	feature := featureQueries
	if id == "getOperation" {
		feature = featureControls
	}
	return definition{
		path:       path,
		id:         id,
		reply:      reply,
		feature:    feature,
		parameters: parameters,
		status:     "200",
	}
}

func mutation(path, id string, reply, input proto.Message, status, description string) definition {
	return definition{
		path:        path,
		id:          id,
		reply:       reply,
		input:       input,
		write:       true,
		feature:     featureControls,
		status:      status,
		description: description,
	}
}

func (b *schemaBuilder) operation(d definition) *Operation {
	operation := &Operation{
		OperationID: d.id,
		Summary:     d.id,
		Description: "Current persistent viewer session is revalidated; responses are observations, not trading authority. " + d.description,
		Parameters:  append(pathParameters(d.path), d.parameters...),
		Responses:   failureResponses(),
		Security: []Security{
			{"secureSession": {}},
			{"localSession": {}},
		},
		Feature:     d.feature,
		MinimumRole: string(domain.AdminRoleViewer),
	}
	if d.write {
		operation.Parameters = append(operation.Parameters, writeHeaders(d.id)...)
		operation.Description += " Exact configured Origin required. JSON body names are unique, with no unknown names or trailing data. Business writes reject all query parameters; optimistic revisions and idempotency bind the actor and original normalized command."
		if d.feature == featureControls {
			operation.MinimumRole = string(domain.AdminRoleOperator)
		}
	}
	if d.id == "login" {
		operation.Security = []Security{}
		operation.MinimumRole = "anonymous"
		operation.Description = "Exact configured Origin required; strict username/password JSON. Sets independent session (HttpOnly) and CSRF (readable) cookies. No secret is returned in the body. Username 3–64 UTF-8 bytes; password 12–72 bytes."
	}
	if d.id == "logout" {
		operation.Description = "Revokes current session and clears both cookies. Same Origin and independently delivered CSRF proof required; no idempotency key or request body is required."
	}
	if d.id == "getServiceOverview" {
		operation.Description += " Default role strategy and limit 4. LOCAL_API observes this API process only (total 1); NACOS_HEALTHY_GRPC shows the selected role membership plus bounded named-instance standard-health probes. Declared version/readiness do not attest ownership. Unconfigured probes omit probed_at_ms."
	}
	if d.id == "getStrategyOverview" {
		operation.Description += " Totals cover the full selected account/requested-source scope independently of the page. Null lease, never-claimed zero epoch, and released holder records are distinct. Lease booleans refer to the sampled instant only."
	}
	if !d.write && d.feature != featureAuth {
		operation.Description += " GET body must be empty. Only listed single-value query names are accepted; positive IDs and nonnegative cursors are canonical int64 strings. Default page limit 50 (maximum 100), except service overview."
	}
	var reply *Schema
	if d.reply != nil {
		reply = b.message(d.reply.ProtoReflect().Descriptor(), true)
	} else if d.replyName != "" {
		reply = reference(d.replyName)
	}
	response := successResponse(reply, d.write && d.feature == featureControls)
	operation.Responses[d.status] = response
	if d.status == "202" {
		operation.Responses["200"] = response
	}
	var input *Schema
	switch {
	case d.input != nil:
		input = b.message(d.input.ProtoReflect().Descriptor(), false)
	case d.bodyName != "":
		input = reference(d.bodyName)
	case d.id == "controlStrategy":
		input = reference("ControlInput")
	case d.id == "changeStrategyRisk":
		input = reference("RiskInput")
	}
	if input != nil {
		operation.RequestBody = &RequestBody{
			Required: true,
			Content:  map[string]MediaType{jsonMediaType: {Schema: input}},
		}
	}
	return operation
}

func successResponse(schema *Schema, location bool) Response {
	response := Response{
		Description: "Successful complete response; Cache-Control: no-store.",
		Headers: map[string]Header{
			"Cache-Control": {
				Description: "Always no-store.",
				Schema:      stringsEnum("no-store"),
			},
		},
	}
	if schema != nil {
		response.Content = map[string]MediaType{jsonMediaType: {Schema: schema}}
	}
	if location {
		response.Headers["Location"] = Header{
			Description: "Relative management resource or result path; not a promise of execution completion.",
			Schema:      text(),
		}
	}
	return response
}

func failureResponses() map[string]Response {
	result := make(map[string]Response)
	for code, description := range map[string]string{
		"400": "Invalid request shape, query, identifier or business input.",
		"401": "No valid current persistent session, or invalid login.",
		"403": "Fixed host/origin, current role or CSRF proof denied.",
		"404": "Feature disabled or management resource not found.",
		"405": "Method not allowed for this route.",
		"409": "Idempotency/revision/state conflict; risk recovery may be denied.",
		"503": "Dependency, timeout or invalid successful upstream response; no partial private payload.",
	} {
		result[code] = Response{
			Description: description,
			Content:     map[string]MediaType{jsonMediaType: {Schema: reference("ErrorReply")}},
		}
	}
	return result
}

func pathParameters(path string) []Parameter {
	var result []Parameter
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			result = append(result, Parameter{
				Name:     strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}"),
				In:       "path",
				Required: true,
				Schema:   int64String(true),
			})
		}
	}
	return result
}

func parameter(name string, required bool, schema *Schema) Parameter {
	return Parameter{
		Name:     name,
		In:       "query",
		Required: required,
		Schema:   schema,
	}
}

func page() []Parameter {
	minimum, maximum := int64(1), int64(100)
	return []Parameter{
		parameter("after_id", false, &Schema{
			Type:        []string{"string"},
			Format:      integerStringFormat,
			Pattern:     `^(0|[1-9][0-9]*)$`,
			Description: "Canonical nonnegative int64 cursor; default 0.",
		}),
		parameter("limit", false, &Schema{
			Type:        []string{"integer"},
			Minimum:     &minimum,
			Maximum:     &maximum,
			Description: "Canonical positive decimal query text; default 50, maximum 100.",
		}),
	}
}

func paged(parameters ...Parameter) []Parameter {
	return append(page(), parameters...)
}

func marketModes() *Schema {
	return stringsEnum(string(domain.MarketModeLive), string(domain.MarketModeReplay))
}

func marketSources() *Schema {
	return stringsEnum(string(domain.MarketSourceBinance), string(domain.MarketSourceOKX))
}

func orderStates() *Schema {
	return stringsEnum(string(domain.OrderStateIntent), string(domain.OrderStateSubmitting), string(domain.OrderStateLive), string(domain.OrderStatePartiallyFilled), string(domain.OrderStateFilled), string(domain.OrderStateCanceled), string(domain.OrderStateRejected), string(domain.OrderStateUnknown))
}

func riskEventParameters() []Parameter {
	return paged(
		parameter("strategy_id", false, int64String(true)),
		parameter("account_id", false, text()),
		parameter("instrument_id", false, int64String(true)),
		parameter("market_data_source", false, marketSources()),
		parameter("kind", false, text()),
		parameter("reason_code", false, text()),
	)
}

func serviceParameters() []Parameter {
	minimum, maximum := int64(1), int64(domain.MaxAdminServicePage)
	return []Parameter{
		parameter("role", false, stringsEnum(string(domain.AdminServiceAPI), string(domain.AdminServiceMarket), string(domain.AdminServiceStrategy), string(domain.AdminServiceExecution), string(domain.AdminServicePortfolio), string(domain.AdminServiceCron))),
		parameter("after_instance_id", false, &Schema{
			Type:        []string{"string"},
			Description: "Exclusive instance label cursor, at most 128 UTF-8 bytes.",
		}),
		parameter("limit", false, &Schema{
			Type:    []string{"integer"},
			Minimum: &minimum,
			Maximum: &maximum,
		}),
	}
}

func writeHeaders(id string) []Parameter {
	result := []Parameter{
		{
			Name:        "Origin",
			In:          "header",
			Required:    true,
			Description: "Must exactly equal the configured fixed administration origin.",
			Schema:      text(),
		},
	}
	if id == "login" {
		return result
	}
	result = append(result, Parameter{
		Name:        "X-CSRF-Token",
		In:          "header",
		Required:    true,
		Description: "Independent readable __Host-muskdeer_csrf (HTTPS) or muskdeer_csrf (local) cookie, bound to the current session.",
		Schema: &Schema{
			Type:    []string{"string"},
			Pattern: `^[A-Za-z0-9_-]{43}$`,
		},
	})
	if id != "logout" {
		result = append(result, Parameter{
			Name:        "Idempotency-Key",
			In:          "header",
			Required:    true,
			Description: "Actor-scoped stable key for retries of the same normalized command; never reuse with a different command.",
			Schema: &Schema{
				Type:      []string{"string"},
				MinLength: 16,
				MaxLength: 64,
				Pattern:   `^[A-Za-z0-9_.:-]+$`,
			},
		})
	}
	return result
}
