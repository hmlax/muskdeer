package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/portalscope"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func TestPortalLoginSelectionAndRejectedRefreshDoNotConsumeAnotherSession(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	base := context.Background()
	tenantID := e.seedTenant(t, "Portal merchant", "PORTAL_A", operatorV1.Tenant_ON)
	otherID := e.seedTenant(t, "Other merchant", "PORTAL_B", operatorV1.Tenant_ON)
	permissionID := e.seedBackendAccessPermission(t)
	roleID := e.seedRole(t, trans.Ptr(tenantID), operatorV1.Role_TENANT, constants.TenantAdminRoleCode, []uint32{permissionID}, nil, nil)
	e.seedCredential(t, trans.Ptr(tenantID), 7401, "portal-user", operatorV1.UserCredential_ENABLED)
	e.seedStubUser(7401, trans.Ptr(tenantID), "portal-user", operatorV1.User_NORMAL, []uint32{roleID})
	scope := portalscope.Scope{
		TenantID: tenantID,
		Code:     "PORTAL_A",
	}
	ctx, request := e.loginHappyReq(t, portalscope.NewContext(base, scope), "portal-user", constants.DefaultUserPassword)
	request.TenantCode = trans.Ptr("IGNORED_BODY_CODE")
	login, err := e.svc.Login(ctx, request)
	require.NoError(t, err)
	require.NotEmpty(t, login.GetAccessToken())

	ctx, request = e.loginHappyReq(t, portalscope.NewContext(base, portalscope.Scope{}), "portal-user", constants.DefaultUserPassword)
	request.TenantCode = trans.Ptr(scope.Code)
	_, err = e.svc.Login(ctx, request)
	require.Error(t, err, "a request body cannot select a merchant from the platform portal")

	_, refresh, err := e.svc.authenticator.CreateUserToken(base, operatorV1.ClientType_admin, &operatorV1.UserTokenPayload{
		UserId:   7401,
		TenantId: trans.Ptr(tenantID),
		Username: trans.Ptr("portal-user"),
	})
	require.NoError(t, err)
	otherScope := portalscope.Scope{
		TenantID: otherID,
		Code:     "PORTAL_B",
	}
	header := http.Header{}
	header.Set("Cookie", "refresh_token_PORTAL_B="+refresh)
	_, err = e.svc.RefreshToken(portalscope.NewContext(headerCtx(base, header), otherScope), &operatorV1.LoginRequest{GrantType: operatorV1.GrantType_refresh_token})
	require.Error(t, err)

	header.Set("Cookie", "refresh_token_PORTAL_A="+refresh)
	reply, err := e.svc.RefreshToken(portalscope.NewContext(headerCtx(base, header), scope), &operatorV1.LoginRequest{GrantType: operatorV1.GrantType_refresh_token})
	require.NoError(t, err, "a wrong-portal request must not consume the valid refresh token")
	require.NotEmpty(t, reply.GetAccessToken())
}
