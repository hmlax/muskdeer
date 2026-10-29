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
)

func (r *Routes) listAudits(ctx context.Context, request *app.RequestContext) {
	filter, valid := auditFilterQuery(request)
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
	reply, err := r.audits.ListAudits(call, &strategyv1.ListAuditsRequest{
		Session:  session,
		AfterId:  filter.Page.AfterID,
		Limit:    int32(filter.Page.Limit),
		Actor:    filter.Actor,
		Action:   filter.Action,
		Resource: filter.Resource,
	})
	r.strategyReply(request, reply, err)
}

func (r *Routes) getAudit(ctx context.Context, request *app.RequestContext) {
	value := request.Param("auditID")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.audits.GetAudit(call, &strategyv1.GetAuditRequest{
		Session: session,
		AuditId: id,
	})
	if err == nil && (reply == nil || reply.Audit == nil || reply.Audit.Id != id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func auditFilterQuery(request *app.RequestContext) (domain.AdminAuditFilter, bool) {
	var filter domain.AdminAuditFilter
	if len(request.URI().QueryString()) > 1024 || len(request.Request.Body()) != 0 {
		return filter, false
	}
	values, err := url.ParseQuery(string(request.URI().QueryString()))
	if err != nil {
		return filter, false
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{
			name:  "actor",
			value: &filter.Actor,
		},
		{
			name:  "action",
			value: &filter.Action,
		},
		{
			name:  "resource",
			value: &filter.Resource,
		},
	} {
		if entries, exists := values[field.name]; exists {
			if len(entries) != 1 {
				return filter, false
			}
			*field.value = entries[0]
			values.Del(field.name)
		}
	}
	var valid bool
	filter.Page, valid = adminPageValues(values)
	return filter, valid && filter.Validate() == nil
}
