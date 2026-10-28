package adminhttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const strategyReadTimeout = 3 * time.Second

func (r *Routes) listStrategies(ctx context.Context, request *app.RequestContext) {
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
	reply, err := r.strategies.ListStrategies(call, &strategyv1.ListStrategiesRequest{
		Session: session,
		AfterId: page.AfterID,
		Limit:   int32(page.Limit),
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) getStrategy(ctx context.Context, request *app.RequestContext) {
	id, valid := strategyID(request)
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
	reply, err := r.strategies.GetStrategy(call, &strategyv1.GetStrategyRequest{
		Session:    session,
		StrategyId: id,
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) listStrategyVersions(ctx context.Context, request *app.RequestContext) {
	id, validID := strategyID(request)
	page, valid := strategyPage(request)
	if !validID || !valid {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.strategies.ListConfigVersions(call, &strategyv1.ListConfigVersionsRequest{
		Session:    session,
		StrategyId: id,
		AfterId:    page.AfterID,
		Limit:      int32(page.Limit),
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) strategySession(request *app.RequestContext) (*strategyv1.AdminSession, bool) {
	token, valid := singleCookie(request, r.sessionCookie)
	if !valid {
		r.failure(request, domain.ErrUnauthenticated)
		return nil, false
	}
	return &strategyv1.AdminSession{Token: token}, true
}

func (r *Routes) strategyReply(request *app.RequestContext, reply proto.Message, err error) {
	if err == nil && reply != nil && reply.ProtoReflect().IsValid() && proto.Size(reply) <= 256*1024 {
		// This is the actual HTTP serialization boundary, with typed business
		// messages throughout. int64 and decimal values remain JSON strings.
		data, encodeErr := (protojson.MarshalOptions{
			UseProtoNames:   true,
			EmitUnpopulated: true,
		}).Marshal(reply)
		if encodeErr == nil {
			request.Data(http.StatusOK, "application/json; charset=utf-8", data)
			return
		}
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		r.failure(request, domain.ErrUnauthenticated)
	case codes.PermissionDenied:
		r.failure(request, domain.ErrForbidden)
	case codes.InvalidArgument:
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
	case codes.NotFound:
		writeError(request, http.StatusNotFound, codeNotFound, messageNotFound)
	default:
		writeError(request, http.StatusServiceUnavailable, codeUnavailable, domain.MessageAdminUnavailable)
	}
}

func strategyID(request *app.RequestContext) (int64, bool) {
	id, err := strconv.ParseInt(request.Param("strategyID"), 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == request.Param("strategyID")
}

func strategyPage(request *app.RequestContext) (domain.AdminPage, bool) {
	page := domain.AdminPage{Limit: 50}
	if len(request.URI().QueryString()) > 256 || len(request.Request.Body()) != 0 {
		return page, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return page, false
	}
	for name, entries := range values {
		if len(entries) != 1 {
			return page, false
		}
		value, err := strconv.ParseInt(entries[0], 10, 64)
		if err != nil || strconv.FormatInt(value, 10) != entries[0] {
			return page, false
		}
		switch name {
		case "after_id":
			if value < 0 {
				return page, false
			}
			page.AfterID = value
		case "limit":
			if value < 1 || value > domain.MaxAdminPageSize {
				return page, false
			}
			page.Limit = int(value)
		default:
			return page, false
		}
	}
	return page, true
}
