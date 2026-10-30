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

func (r *Routes) listStrategyTrades(ctx context.Context, request *app.RequestContext) {
	filter, valid := tradeFilterQuery(request)
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
	reply, err := r.trades.ListStrategyTrades(call, &strategyv1.ListStrategyTradesRequest{
		Session:    session,
		StrategyId: filter.StrategyID,
		AfterId:    filter.Page.AfterID,
		Limit:      int32(filter.Page.Limit),
		OrderId:    filter.OrderID,
		Side:       string(filter.Side),
	})
	if err == nil && !strategywire.ValidTradePage(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) getStrategyTrade(ctx context.Context, request *app.RequestContext) {
	strategy, valid := strategyID(request)
	id, idValid := tradePathID(request.Param("tradeID"))
	if !valid || !idValid || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.trades.GetStrategyTrade(call, &strategyv1.GetStrategyTradeRequest{
		Session:    session,
		StrategyId: strategy,
		TradeId:    id,
	})
	if err == nil && (reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || !strategywire.ValidTrade(reply.Trade, strategy) || reply.Trade.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func tradePathID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func tradeFilterQuery(request *app.RequestContext) (domain.AdminTradeFilter, bool) {
	var filter domain.AdminTradeFilter
	id, valid := strategyID(request)
	if !valid || len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	filter.StrategyID = id
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	if entries, exists := values["side"]; exists {
		if len(entries) != 1 || entries[0] != string(domain.SideBuy) && entries[0] != string(domain.SideSell) {
			return filter, false
		}
		filter.Side = domain.Side(entries[0])
		values.Del("side")
	}
	if entries, exists := values["order_id"]; exists {
		if len(entries) != 1 {
			return filter, false
		}
		orderID, err := strconv.ParseInt(entries[0], 10, 64)
		if err != nil || orderID <= 0 || strconv.FormatInt(orderID, 10) != entries[0] {
			return filter, false
		}
		filter.OrderID = orderID
		values.Del("order_id")
	}
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
