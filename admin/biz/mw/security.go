package mw

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	authzEngine "github.com/tx7do/kratos-authz/engine"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const (
	tenantActive       = "ON"
	errorTenantExpired = "tenant has expired"

	errorAccessDenied       = "access denied"
	errorUnauthorizedAccess = "unauthorized access"
	errorTenantInactive     = "tenant is not active"

	ErrorPlatformRequired = "platform administrator required"
)

type tenantChecker struct {
	client operatorV1.GatewayServiceClient
}

func (s *tenantChecker) CheckTenantAccess(ctx context.Context, id uint32, path, method string) error {
	data, err := s.client.GetTenantAccessData(ctx, &operatorV1.TenantAccessRequest{
		TenantId: id,
		Path:     strings.TrimSuffix(path, ":export"),
		Method:   method,
	})
	if err != nil {
		return err
	}
	return checkTenantAccess(data, time.Now())
}
func checkTenantAccess(data *operatorV1.TenantAccessData, now time.Time) error {
	if data == nil || !data.GetTenantExists() {
		return operatorV1.ErrorAdminForbidden(errorAccessDenied)
	}
	if data.Status != nil && data.GetStatus() != tenantActive {
		return operatorV1.ErrorAdminForbidden(errorTenantInactive)
	}
	if data.ExpiresAt != nil && data.GetExpiresAt() <= now.Unix() {
		return operatorV1.ErrorAdminForbidden(errorTenantExpired)
	}
	if !data.GetApiExists() {
		return operatorV1.ErrorAdminForbidden(errorAccessDenied)
	}
	return nil
}

var platformServices = map[string]bool{"TenantService": true}

type policiesKey struct{}

func checkBoundary(ctx context.Context, exchange *httpmeta.Exchange, client operatorV1.GatewayServiceClient) (context.Context, error) {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return ctx, err
	}
	parts := strings.Split(strings.TrimPrefix(exchange.OperationName, "/operator.v1."), "/")
	platformOnly := platformServices[parts[0]] || exchange.OperationName == operatorV1.OperationExportServiceExportAuditLogs
	if platformOnly && !actor.GetIsPlatformAdmin() {
		return ctx, operatorV1.ErrorAdminForbidden(ErrorPlatformRequired)
	}
	if !actor.GetIsPlatformAdmin() {
		policies, err := client.GetPolicies(ctx, &emptypb.Empty{})
		if err != nil {
			return ctx, err
		}
		ctx = context.WithValue(ctx, policiesKey{}, policies)
	}
	return ctx, nil
}

type policyEngine struct{}

func (*policyEngine) Name() string { return "gateway-rbac" }
func (*policyEngine) IsAuthorized(ctx context.Context, subject authzEngine.Subject, action authzEngine.Action, resource authzEngine.Resource, project authzEngine.Project) (bool, error) {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return false, err
	}
	if actor.GetIsPlatformAdmin() {
		return true, nil
	}
	policies, _ := ctx.Value(policiesKey{}).(*operatorV1.PoliciesResponse)
	if policies == nil {
		return false, nil
	}
	resource = authzEngine.Resource(strings.TrimSuffix(string(resource), ":export"))
	domain := strconv.FormatUint(uint64(actor.GetTenantId()), 10)
	if project != "" && string(project) != domain {
		return false, nil
	}
	for _, role := range policies.GetPolicies() {
		if role.GetRole() != string(subject) {
			continue
		}
		for _, permission := range role.GetPermissions() {
			if permission.GetDomain() != domain {
				continue
			}
			if permission.GetPath() != string(resource) && permission.GetPath() != "*" {
				continue
			}
			if permission.GetMethod() == string(action) || permission.GetMethod() == "*" {
				return true, nil
			}
		}
	}
	return false, nil
}
func (s *policyEngine) ProjectsAuthorized(ctx context.Context, subjects authzEngine.Subjects, action authzEngine.Action, resource authzEngine.Resource, projects authzEngine.Projects) (authzEngine.Projects, error) {
	out := make(authzEngine.Projects, 0)
	for _, project := range projects {
		for _, subject := range subjects {
			allowed, err := s.IsAuthorized(ctx, subject, action, resource, project)
			if err != nil {
				return nil, err
			}
			if allowed {
				out = append(out, project)
				break
			}
		}
	}
	return out, nil
}
func (s *policyEngine) FilterAuthorizedPairs(ctx context.Context, subjects authzEngine.Subjects, pairs authzEngine.Pairs) (authzEngine.Pairs, error) {
	out := make(authzEngine.Pairs, 0)
	for _, pair := range pairs {
		for _, subject := range subjects {
			allowed, err := s.IsAuthorized(ctx, subject, pair.Action, pair.Resource, "")
			if err != nil {
				return nil, err
			}
			if allowed {
				out = append(out, pair)
				break
			}
		}
	}
	return out, nil
}
func (s *policyEngine) FilterAuthorizedProjects(ctx context.Context, subjects authzEngine.Subjects) (authzEngine.Projects, error) {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	if actor.GetIsPlatformAdmin() {
		return authzEngine.Projects{"*"}, nil
	}
	domain := strconv.FormatUint(uint64(actor.GetTenantId()), 10)
	policies, _ := ctx.Value(policiesKey{}).(*operatorV1.PoliciesResponse)
	for _, role := range policies.GetPolicies() {
		for _, subject := range subjects {
			if role.GetRole() != string(subject) {
				continue
			}
			for _, permission := range role.GetPermissions() {
				if permission.GetDomain() == domain {
					return authzEngine.Projects{authzEngine.Project(domain)}, nil
				}
			}
		}
	}
	return authzEngine.Projects{}, nil
}

type policyRepo struct {
	client operatorV1.GatewayServiceClient
}

func (r *policyRepo) Create(ctx context.Context, req *operatorV1.CreatePolicyEvaluationLogRequest) error {
	_, err := r.client.WriteAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_PolicyEvaluation{PolicyEvaluation: req.GetData()}})
	if err != nil {
		slog.ErrorContext(ctx, "write policy evaluation audit", "error", err)
	}
	return err
}
func (r *policyRepo) ResolvePermissionPolicyByRoute(ctx context.Context, subject, path, method string) (uint32, uint32) {
	result, err := r.client.ResolvePolicy(ctx, &operatorV1.ResolvePolicyRequest{
		Subject: subject,
		Path:    strings.TrimSuffix(path, ":export"),
		Method:  method,
	})
	if err != nil {
		slog.ErrorContext(ctx, "resolve permission policy", "error", err)
		return 0, 0
	}
	return result.GetPermissionId(), result.GetPolicyId()
}

// NewTenantChecker evaluates tenant access using trusted operator facts.
func NewTenantChecker(client operatorV1.GatewayServiceClient) auth.TenantAccessChecker {
	return &tenantChecker{client: client}
}
