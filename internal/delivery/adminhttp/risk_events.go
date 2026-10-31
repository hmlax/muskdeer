package adminhttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

func (r *Routes) listRiskEvents(ctx context.Context, request *app.RequestContext) {
	filter, valid := riskEventFilterQuery(request)
	if !valid {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.riskEvents.ListRiskEvents(call, &strategyv1.ListRiskEventsRequest{
		Session:          session,
		AfterId:          filter.Page.AfterID,
		Limit:            int32(filter.Page.Limit),
		StrategyId:       filter.StrategyID,
		AccountId:        filter.AccountID,
		InstrumentId:     filter.InstrumentID,
		MarketDataSource: string(filter.Source),
		Kind:             string(filter.Kind),
		ReasonCode:       string(filter.Reason),
	})
	if err == nil && !strategywire.ValidRiskEventPage(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) getRiskEvent(ctx context.Context, request *app.RequestContext) {
	id, valid := riskEventPathID(request.Param("eventID"))
	if !valid || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.riskEvents.GetRiskEvent(call, &strategyv1.GetRiskEventRequest{
		Session: session,
		EventId: id,
	})
	if err == nil && (reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || !strategywire.ValidRiskEvent(reply.Event) || reply.Event.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func riskEventPathID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func riskEventFilterQuery(request *app.RequestContext) (domain.AdminRiskEventFilter, bool) {
	var filter domain.AdminRiskEventFilter
	if len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	for _, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return filter, false
		}
	}
	filter.AccountID = values.Get("account_id")
	filter.Source = domain.MarketSource(values.Get("market_data_source"))
	filter.Kind = domain.RiskSnapshotEventKind(values.Get("kind"))
	filter.Reason = domain.RiskReason(values.Get("reason_code"))
	for _, field := range []struct {
		name string
		into *int64
	}{
		{
			name: "strategy_id",
			into: &filter.StrategyID,
		},
		{
			name: "instrument_id",
			into: &filter.InstrumentID,
		},
	} {
		if raw := values.Get(field.name); raw != "" {
			id, valid := riskEventPathID(raw)
			if !valid {
				return filter, false
			}
			*field.into = id
		}
		values.Del(field.name)
	}
	for _, key := range []string{"account_id", "market_data_source", "kind", "reason_code"} {
		values.Del(key)
	}
	var valid bool
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
