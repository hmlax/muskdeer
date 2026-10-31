package adminhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/servicewire"
)

func (r *Routes) getServiceOverview(ctx context.Context, request *app.RequestContext) {
	filter, valid := serviceFilterQuery(request)
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
	value, err := r.services.Observe(call, session.Token, filter)
	if err != nil {
		r.serviceFailure(request, err)
		return
	}
	reply, err := servicewire.OverviewProto(value, filter)
	if err != nil {
		r.serviceFailure(request, err)
		return
	}
	r.strategyReply(request, reply, nil)
}

func (r *Routes) serviceFailure(request *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		r.failure(request, domain.ErrUnauthenticated)
	case errors.Is(err, domain.ErrForbidden):
		r.failure(request, domain.ErrForbidden)
	default:
		writeError(request, http.StatusServiceUnavailable, codeUnavailable, domain.MessageAdminUnavailable)
	}
}

func serviceFilterQuery(request *app.RequestContext) (domain.AdminServiceFilter, bool) {
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: domain.MaxAdminServicePage,
	}
	if len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return filter, false
		}
		switch key {
		case "role":
			filter.Role = domain.AdminServiceRole(entries[0])
		case "after_instance_id":
			filter.AfterInstanceID = entries[0]
		case "limit":
			number, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || strconv.FormatInt(number, 10) != entries[0] || number < 1 || number > domain.MaxAdminServicePage {
				return filter, false
			}
			filter.Limit = int(number)
		default:
			return filter, false
		}
	}
	return filter, filter.Validate() == nil
}
