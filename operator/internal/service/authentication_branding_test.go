package service

import (
	"testing"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
)

// TestGetTenantBranding 白标查询：命中返回展示字段、未命中 found=false
// （不区分"不存在"与"未设置"，不给枚举租户编号的探测者命中信号）、空编号直接 false。
func TestGetTenantBranding(t *testing.T) {
	e := newAuthenticationServiceForTest(t)

	// seedTenant 不带 LogoUrl——直接落一条带 Logo 的
	_, err := e.svc.tenantRepo.Create(e.ctx, &operatorV1.Tenant{
		Name:    trans.Ptr("白标租户甲"),
		Code:    trans.Ptr("BRAND_A"),
		LogoUrl: trans.Ptr("https://cdn.example.com/a.png"),
		Status:  operatorV1.Tenant_ON.Enum(),
	})
	require.NoError(t, err)
	_, err = e.svc.tenantRepo.Create(e.ctx, &operatorV1.Tenant{
		Name:   trans.Ptr("无Logo租户乙"),
		Code:   trans.Ptr("BRAND_B"),
		Status: operatorV1.Tenant_ON.Enum(),
	})
	require.NoError(t, err)

	// 命中带 Logo 的
	resp, err := e.svc.GetTenantBranding(e.ctx, &operatorV1.GetTenantBrandingRequest{Code: "BRAND_A"})
	require.NoError(t, err)
	require.True(t, resp.GetFound())
	require.Equal(t, "白标租户甲", resp.GetName())
	require.Equal(t, "https://cdn.example.com/a.png", resp.GetLogoUrl())

	// 命中但未设置 Logo：LogoUrl 空串（前端保持默认 Logo）
	resp, err = e.svc.GetTenantBranding(e.ctx, &operatorV1.GetTenantBrandingRequest{Code: "BRAND_B"})
	require.NoError(t, err)
	require.True(t, resp.GetFound())
	require.Equal(t, "无Logo租户乙", resp.GetName())
	require.Empty(t, resp.GetLogoUrl())

	// 未命中：found=false 而非错误（防枚举）
	resp, err = e.svc.GetTenantBranding(e.ctx, &operatorV1.GetTenantBrandingRequest{Code: "NO_SUCH"})
	require.NoError(t, err)
	require.False(t, resp.GetFound())
	require.Empty(t, resp.GetName())

	// 空编号：直接 false，不查库
	resp, err = e.svc.GetTenantBranding(e.ctx, &operatorV1.GetTenantBrandingRequest{Code: "  "})
	require.NoError(t, err)
	require.False(t, resp.GetFound())
}
