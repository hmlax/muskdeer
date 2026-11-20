package mw

import (
	"context"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/portalscope"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func ResolvePortal(ctx context.Context, gateway operatorV1.GatewayServiceClient) (context.Context, error) {
	exchange, ok := httpmeta.FromContext(ctx)
	if !ok {
		return nil, auth.ErrWrongContext
	}
	code, err := portalscope.ParseCode(exchange.RequestValue.Header.Values(portalscope.TenantCodeHeader))
	if err != nil {
		return nil, err
	}
	scope := portalscope.Scope{Code: code}
	if code != "" {
		row, err := gateway.ResolveTenantCode(ctx, &operatorV1.ResolveTenantCodeRequest{Code: code})
		if err != nil {
			return nil, err
		}
		if row.GetTenantId() == 0 {
			return nil, operatorV1.ErrorAdminForbidden(portalscope.ErrorUnavailable)
		}
		scope.TenantID = row.GetTenantId()
	}
	ctx = portalscope.NewContext(ctx, scope)
	// The outer audit recorder finishes with its original context. Preserve the
	// validated portal on the request so anonymous login audits retain tenancy.
	exchange.RequestValue = exchange.RequestValue.WithContext(ctx)
	return ctx, nil
}
