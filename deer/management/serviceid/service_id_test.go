package serviceid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewDiscoveryName_AdminService 钉住 API 的逻辑服务发现名称。
func TestNewDiscoveryName_AdminService(t *testing.T) {
	got := NewDiscoveryName(AdminService)
	assert.Equal(t, "admin", got)
}

// TestNewDiscoveryName_UsesLogicalName 不添加项目名前缀。
func TestNewDiscoveryName_UsesLogicalName(t *testing.T) {
	got := NewDiscoveryName("foo")
	assert.Equal(t, "foo", got)
	assert.NotContains(t, got, "/")
}

// TestProjectNameConstant 钉住项目名常量值，防止被误改影响服务发现。
func TestProjectNameConstant(t *testing.T) {
	assert.Equal(t, "go-admin", ProjectName)
}

// TestAdminServiceConstant 钉住 AdminService 常量值。
func TestAdminServiceConstant(t *testing.T) {
	assert.Equal(t, "admin", AdminService)
}
