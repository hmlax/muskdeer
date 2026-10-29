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

func (r *Routes) listStrategyOrders(ctx context.Context, request *app.RequestContext) {
	filter, valid := orderFilterQuery(request)
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
	reply, err := r.orders.ListStrategyOrders(call, &strategyv1.ListStrategyOrdersRequest{
		Session:    session,
		StrategyId: filter.StrategyID,
		AfterId:    filter.Page.AfterID,
		Limit:      int32(filter.Page.Limit),
		State:      string(filter.State),
		OpenOnly:   filter.OpenOnly,
	})
	if err == nil && !strategywire.ValidOrderPage(reply, filter) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) getStrategyOrder(ctx context.Context, request *app.RequestContext) {
	strategy, valid := strategyID(request)
	raw := request.Param("orderID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if !valid || err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.orders.GetStrategyOrder(call, &strategyv1.GetStrategyOrderRequest{
		Session:    session,
		StrategyId: strategy,
		OrderId:    id,
	})
	if err == nil && (reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || !strategywire.ValidOrder(reply.Order, strategy) || reply.Order.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func orderFilterQuery(request *app.RequestContext) (domain.AdminOrderFilter, bool) {
	var filter domain.AdminOrderFilter
	id, valid := strategyID(request)
	if !valid || len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	filter.StrategyID = id
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	if entries, exists := values["state"]; exists {
		if len(entries) != 1 || !domain.ValidOrderState(domain.OrderState(entries[0])) {
			return filter, false
		}
		filter.State = domain.OrderState(entries[0])
		values.Del("state")
	}
	if entries, exists := values["open_only"]; exists {
		if len(entries) != 1 || entries[0] != "true" && entries[0] != "false" {
			return filter, false
		}
		filter.OpenOnly = entries[0] == "true"
		values.Del("open_only")
	}
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
