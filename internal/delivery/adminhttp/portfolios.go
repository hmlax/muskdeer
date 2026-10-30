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

func (r *Routes) listPortfolios(ctx context.Context, request *app.RequestContext) {
	filter, valid := portfolioFilterQuery(request)
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
	reply, err := r.portfolios.ListPortfolios(call, &strategyv1.ListPortfoliosRequest{
		Session:      session,
		AfterId:      filter.Page.AfterID,
		Limit:        int32(filter.Page.Limit),
		AccountId:    filter.AccountID,
		InstrumentId: filter.InstrumentID,
	})
	if err == nil && !strategywire.ValidPortfolioPage(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) getPortfolio(ctx context.Context, request *app.RequestContext) {
	id, valid := portfolioPathID(request.Param("portfolioID"))
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
	reply, err := r.portfolios.GetPortfolio(call, &strategyv1.GetPortfolioRequest{
		Session:     session,
		PortfolioId: id,
	})
	if err == nil && (reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || !strategywire.ValidPortfolio(reply.Portfolio) || reply.Portfolio.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func portfolioPathID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func portfolioFilterQuery(request *app.RequestContext) (domain.AdminPortfolioFilter, bool) {
	var filter domain.AdminPortfolioFilter
	if len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	if entries, exists := values["account_id"]; exists {
		if len(entries) != 1 || entries[0] == "" {
			return filter, false
		}
		filter.AccountID = entries[0]
		values.Del("account_id")
	}
	if entries, exists := values["instrument_id"]; exists {
		if len(entries) != 1 {
			return filter, false
		}
		id, valid := portfolioPathID(entries[0])
		if !valid {
			return filter, false
		}
		filter.InstrumentID = id
		values.Del("instrument_id")
	}
	var valid bool
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
