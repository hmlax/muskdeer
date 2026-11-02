package adminhttp

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hmlax/muskdeer/internal/adminactor"
	"github.com/hmlax/muskdeer/internal/domain"
)

const actorRequestKey = "muskdeer.validated.actor"

func (r *Routes) authorizeBusiness(ctx context.Context, request *app.RequestContext, token string) bool {
	var actor domain.AdminPrincipal
	var err error
	if string(request.Method()) == "GET" {
		actor, err = r.auth.Identity(ctx, token)
	} else {
		if !r.sameOrigin(request) {
			r.failure(request, domain.ErrForbidden)
			return false
		}
		proof := request.Request.Header.GetAll(CSRFHeader)
		if len(proof) != 1 || !cookieSecret(proof[0]) {
			r.failure(request, domain.ErrForbidden)
			return false
		}
		required := domain.AdminRoleOperator
		if strings.HasSuffix(string(request.Path()), "/risk") {
			input, decodeErr := decodeRiskInput(request.Request.Body())
			if decodeErr != nil {
				r.failure(request, domain.ErrForbidden)
				return false
			}
			if domain.AdminRiskAction(input.Action) == domain.AdminRiskUnblock {
				required = domain.AdminRoleAdmin
			}
		}
		actor, err = r.auth.Authorize(ctx, token, proof[0], required)
		if err == nil && !actor.Role.Allows(required) {
			err = domain.ErrForbidden
		}
	}
	if err != nil {
		r.failure(request, err)
		return false
	}
	if _, valid := principalView(actor); !valid {
		r.failure(request, domain.ErrAuthUnavailable)
		return false
	}
	if !actor.Role.Allows(domain.AdminRoleViewer) {
		r.failure(request, domain.ErrForbidden)
		return false
	}
	request.Set(actorRequestKey, actor)
	return true
}

func managementContext(ctx context.Context, request *app.RequestContext) context.Context {
	value, exists := request.Get(actorRequestKey)
	if !exists {
		return ctx
	}
	actor, ok := value.(domain.AdminPrincipal)
	if !ok {
		return ctx
	}
	return adminactor.Outgoing(adminactor.Bind(ctx, actor))
}

func (r *Routes) rpcCSRF(value string) string {
	if r.centralAuth {
		return ""
	}
	return value
}
