package adminhttp

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

func (r *Routes) getStrategyOverview(ctx context.Context, request *app.RequestContext) {
	filter, valid := overviewFilterQuery(request)
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
	reply, err := r.overview.GetStrategyOverview(call, &strategyv1.GetStrategyOverviewRequest{
		Session:                   session,
		AfterId:                   filter.Page.AfterID,
		Limit:                     int32(filter.Page.Limit),
		AccountId:                 filter.AccountID,
		RequestedMarketDataSource: string(filter.Source),
	})
	if err == nil && !strategywire.ValidOverview(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func overviewFilterQuery(request *app.RequestContext) (domain.AdminOverviewFilter, bool) {
	var filter domain.AdminOverviewFilter
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
	filter.Source = domain.MarketSource(values.Get("requested_market_data_source"))
	values.Del("account_id")
	values.Del("requested_market_data_source")
	var valid bool
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
