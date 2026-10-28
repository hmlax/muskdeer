package adminhttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (r *Routes) listAccounts(ctx context.Context, request *app.RequestContext) {
	page, valid := strategyPage(request)
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
	reply, err := r.catalog.ListAccounts(call, &strategyv1.ListAccountsRequest{
		Session: session,
		AfterId: page.AfterID,
		Limit:   int32(page.Limit),
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) listInstruments(ctx context.Context, request *app.RequestContext) {
	page, valid := strategyPage(request)
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
	reply, err := r.catalog.ListInstruments(call, &strategyv1.ListInstrumentsRequest{
		Session: session,
		AfterId: page.AfterID,
		Limit:   int32(page.Limit),
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) listMarketFeeds(ctx context.Context, request *app.RequestContext) {
	id, mode, page, valid := catalogFeedQuery(request)
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
	reply, err := r.catalog.ListMarketFeeds(call, &strategyv1.ListMarketFeedsRequest{
		Session:      session,
		InstrumentId: id,
		Mode:         string(mode),
		AfterId:      page.AfterID,
		Limit:        int32(page.Limit),
	})
	r.strategyReply(request, reply, err)
}

func catalogFeedQuery(request *app.RequestContext) (int64, domain.MarketMode, domain.AdminPage, bool) {
	var page domain.AdminPage
	value := request.Param("instrumentID")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value || len(request.URI().QueryString()) > 256 || len(request.Request.Body()) != 0 {
		return 0, "", page, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil || len(values["mode"]) != 1 {
		return 0, "", page, false
	}
	mode := domain.MarketMode(values.Get("mode"))
	if mode != domain.MarketModeLive && mode != domain.MarketModeReplay {
		return 0, "", page, false
	}
	values.Del("mode")
	page, valid := adminPageValues(values)
	return id, mode, page, valid
}
