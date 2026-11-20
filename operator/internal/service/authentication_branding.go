package service

import (
	"context"

	"github.com/hmlax/muskdeer/deer/management/portalscope"

	"github.com/tx7do/go-crud/viewer"

	"strings"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// GetTenantBranding 租户白标查询（免鉴权，登录前用）。
//
// 只暴露展示字段（name/logo_url）：免鉴权端点的暴露面必须最小，
// 运营/配额/管理员等字段一律不给。编号未命中返回 found=false（而非 404）——
// 登录页对"不存在"与"未设置白标"同等对待：保持默认品牌即可，不区别提示，
// 避免给枚举租户编号的探测者提供命中/未命中的区分信号。
// LogoUrl 为空串收敛（proto3-empty-string-src-attrs：空串直接进 src 会触发告警）。
func (s *AuthenticationService) GetTenantBranding(ctx context.Context, req *operatorV1.GetTenantBrandingRequest) (*operatorV1.GetTenantBrandingResponse, error) {
	code := req.GetCode()
	if scope, ok := portalscope.FromContext(ctx); ok {
		code = scope.Code
	}
	if strings.TrimSpace(code) == "" {
		return &operatorV1.GetTenantBrandingResponse{Found: false}, nil
	}
	ctx = viewer.WithSystemContext(ctx)

	tenant, err := s.tenantRepo.Get(ctx, &operatorV1.GetTenantRequest{
		QueryBy: &operatorV1.GetTenantRequest_Code{Code: code},
	})
	if err != nil || tenant == nil {
		// 未命中不是错误（防枚举），只有基础设施故障才 500——但这里的 err
		// 来自 repo（已带日志），归一为 found=false 即可
		return &operatorV1.GetTenantBrandingResponse{Found: false}, nil
	}

	return &operatorV1.GetTenantBrandingResponse{
		Found:   true,
		Name:    tenant.GetName(),
		LogoUrl: tenant.GetLogoUrl(),
	}, nil
}
