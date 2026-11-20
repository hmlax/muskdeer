package mw

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/portalscope"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type portalLookup struct {
	operatorV1.GatewayServiceClient
	calls int
}

func (s *portalLookup) ResolveTenantCode(_ context.Context, request *operatorV1.ResolveTenantCodeRequest, _ ...grpc.CallOption) (*operatorV1.ResolveTenantCodeResponse, error) {
	s.calls++
	if request.GetCode() != "MerchantA" {
		return nil, operatorV1.ErrorAdminForbidden(portalscope.ErrorUnavailable)
	}
	return &operatorV1.ResolveTenantCodeResponse{TenantId: 7}, nil
}

func TestPortalResolutionUsesFactsAndIgnoresBrowserGatewayMetadata(t *testing.T) {
	request := httptest.NewRequest("GET", "/admin/v1/me", nil)
	request.Header.Set("X-Gateway-Portal-Tenant", "0")
	request.Header.Set(portalscope.TenantCodeHeader, "MerchantA")
	ctx := httpmeta.NewContext(context.Background(), &httpmeta.Exchange{RequestValue: request})
	lookup := &portalLookup{}
	ctx, err := ResolvePortal(ctx, lookup)
	require.NoError(t, err)
	scope, ok := portalscope.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, uint32(7), scope.TenantID)
	exchange, ok := httpmeta.FromContext(ctx)
	require.True(t, ok)
	auditScope, ok := portalscope.FromContext(exchange.Request().Context())
	require.True(t, ok)
	require.Equal(t, scope, auditScope, "outer auditing must receive the resolved portal")
	require.Equal(t, 1, lookup.calls)
	require.Error(t, portalscope.CheckTenant(ctx, 8))
	require.Error(t, portalscope.CheckTenant(ctx, 0))
	require.NoError(t, portalscope.CheckTenant(ctx, 7))

	request.Header.Del(portalscope.TenantCodeHeader)
	ctx, err = ResolvePortal(ctx, lookup)
	require.NoError(t, err)
	require.NoError(t, portalscope.CheckTenant(ctx, 0))
	require.Error(t, portalscope.CheckTenant(ctx, 7))
	require.Equal(t, 1, lookup.calls, "platform requests must not resolve a merchant")

	request.Header.Set(portalscope.TenantCodeHeader, "Unknown")
	_, err = ResolvePortal(ctx, lookup)
	require.Error(t, err)
}
