package adminhttp

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/hmlax/muskdeer/internal/domain"
)

type gateAuth struct {
	domain.AdminAuth
	role     domain.AdminRole
	failure  error
	reads    int
	writes   int
	required domain.AdminRole
}

func (a *gateAuth) Identity(context.Context, string) (domain.AdminPrincipal, error) {
	a.reads++
	value := testPrincipal()
	value.Role = a.role
	return value, a.failure
}
func (a *gateAuth) Authorize(_ context.Context, token, csrf string, required domain.AdminRole) (domain.AdminPrincipal, error) {
	a.writes++
	a.required = required
	if token != testSecret(1) || csrf != testSecret(2) {
		return domain.AdminPrincipal{}, domain.ErrForbidden
	}
	value := testPrincipal()
	value.Role = a.role
	if !a.role.Allows(required) {
		return domain.AdminPrincipal{}, domain.ErrForbidden
	}
	return value, a.failure
}
func centralFixture(t *testing.T, auth *gateAuth, client *controlHTTPClient) *server.Hertz {
	t.Helper()
	routes, err := New(auth, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.centralAuth = true
	routes.strategies = &strategyHTTPClient{}
	routes.controls = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}
func TestAdminAloneChecksManagementAuthorityAndDoesNotForwardSecrets(t *testing.T) {
	auth := &gateAuth{role: domain.AdminRoleOperator}
	client := &controlHTTPClient{}
	hertz := centralFixture(t, auth, client)
	reply := perform(hertz, "POST", "/api/v1/strategies/9007199254740993/control", controlHTTPBody, controlHTTPHeaders()...)
	if reply.Code != 202 || auth.writes != 1 || auth.required != domain.AdminRoleOperator || client.calls != 1 || client.token != "" || client.csrf != "" {
		t.Fatalf("admin authorization/secret boundary: status=%d auth=%d rpc=%d", reply.Code, auth.writes, client.calls)
	}
	for _, role := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator} {
		denied := &gateAuth{
			role:    role,
			failure: domain.ErrUnauthenticated,
		}
		calls := &controlHTTPClient{}
		endpoint := centralFixture(t, denied, calls)
		if result := perform(endpoint, "GET", "/api/v1/strategies", "", ut.Header{
			Key:   "Cookie",
			Value: secureSessionCookie + "=" + testSecret(1),
		}); result.Code != 401 || calls.calls != 0 || denied.reads != 1 {
			t.Fatal("revoked session reached business RPC")
		}
	}
	viewer := &gateAuth{role: domain.AdminRoleViewer}
	calls := &controlHTTPClient{}
	if result := perform(centralFixture(t, viewer, calls), "POST", "/api/v1/strategies/9007199254740993/control", controlHTTPBody, controlHTTPHeaders()...); result.Code != 403 || calls.calls != 0 {
		t.Fatal("viewer write reached business RPC")
	}
}

func TestAdminRejectsRiskRecoveryBeforeRPCForOperatorRole(t *testing.T) {
	auth := &gateAuth{role: domain.AdminRoleOperator}
	client := &riskHTTPClient{}
	routes, err := New(auth, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.centralAuth = true
	routes.riskControls = client
	endpoint := server.New()
	routes.Register(endpoint)
	body := `{"action":"` + string(domain.AdminRiskUnblock) + `","expected_revision":"9007199254740993","expected_config_version":"9007199254740995","expected_desired_state":"` + string(domain.StrategyStatePaused) + `","reason":"explicit restore"}`
	result := perform(endpoint, "POST", riskHTTPPath, body, controlHTTPHeaders()...)
	if result.Code != 403 || auth.required != domain.AdminRoleAdmin || auth.writes != 1 || client.calls != 0 {
		t.Fatalf("risk recovery authority: status=%d required=%s auth=%d rpc=%d", result.Code, auth.required, auth.writes, client.calls)
	}
}
