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

func (r *Routes) listMarkets(ctx context.Context, request *app.RequestContext) {
	filter, valid := marketFilterQuery(request)
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
	reply, err := r.markets.ListMarkets(call, &strategyv1.ListMarketsRequest{
		Session:      session,
		Source:       string(filter.Source),
		InstrumentId: filter.InstrumentID,
		Mode:         string(filter.Mode),
		AfterId:      filter.Page.AfterID,
		Limit:        int32(filter.Page.Limit),
	})
	if err == nil && !strategywire.ValidMarketPage(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) getMarket(ctx context.Context, request *app.RequestContext) {
	raw := request.Param("feedID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.markets.GetMarket(call, &strategyv1.GetMarketRequest{
		Session: session,
		FeedId:  id,
	})
	if err == nil && (reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || !strategywire.ValidMarket(reply.Market) || reply.Market.Feed.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func marketFilterQuery(request *app.RequestContext) (domain.AdminMarketFilter, bool) {
	var filter domain.AdminMarketFilter
	if len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil || len(values["mode"]) != 1 {
		return filter, false
	}
	filter.Mode = domain.MarketMode(values.Get("mode"))
	values.Del("mode")
	if entries, exists := values["source"]; exists {
		if len(entries) != 1 || domain.MarketSource(entries[0]).ValidateSelection() != nil {
			return filter, false
		}
		filter.Source = domain.MarketSource(entries[0])
		values.Del("source")
	}
	if entries, exists := values["instrument_id"]; exists {
		if len(entries) != 1 {
			return filter, false
		}
		filter.InstrumentID, err = strconv.ParseInt(entries[0], 10, 64)
		if err != nil || filter.InstrumentID <= 0 || strconv.FormatInt(filter.InstrumentID, 10) != entries[0] {
			return filter, false
		}
		values.Del("instrument_id")
	}
	var valid bool
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
