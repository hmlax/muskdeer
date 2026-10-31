package adminhttp

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/hmlax/muskdeer/internal/domain"
)

// downloadOpenAPI serves immutable process-local contract bytes only after a
// bounded current identity read through the API's existing auth dependency.
func (r *Routes) downloadOpenAPI(ctx context.Context, request *app.RequestContext) {
	if len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	token, valid := singleCookie(request, r.sessionCookie)
	if !valid {
		r.failure(request, domain.ErrUnauthenticated)
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	principal, err := r.auth.Identity(call, token)
	if err != nil {
		r.failure(request, err)
		return
	}
	if _, valid := principalView(principal); call.Err() != nil || !valid {
		r.failure(request, domain.ErrAuthUnavailable)
		return
	}
	request.Data(http.StatusOK, "application/json; charset=utf-8", r.openAPI)
}
