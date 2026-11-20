package constants

import (
	"testing"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/stretchr/testify/assert"
)

// TestComponentToModule verifies API administration categories.
func TestComponentToModule(t *testing.T) {
	cases := []struct {
		name      string
		component string
		want      operatorV1.Module_Module
	}{
		// catalog 容器节点：不参与白名单过滤
		{"layout container", "BasicLayout", operatorV1.Module_MODULE_UNSPECIFIED},
		{"empty component", "", operatorV1.Module_MODULE_UNSPECIFIED},

		// 登记过的前缀，路径带具体页面
		{"dashboard page", "dashboard/analytics/index.vue", operatorV1.Module_DASHBOARD},
		{"dashboard bare prefix", "dashboard/", operatorV1.Module_DASHBOARD},
		{"opm page", "app/opm/user/list/index.vue", operatorV1.Module_OPM},
		{"system page", "app/system/config/index.vue", operatorV1.Module_SYSTEM},
		{"dict page", "app/dict/entry/index.vue", operatorV1.Module_DICT},
		{"tenant page", "app/tenant/tenant/index.vue", operatorV1.Module_TENANT},
		{"permission page", "app/permission/menu/index.vue", operatorV1.Module_PERMISSION},
		{"log page", "app/log/api_audit_log/index.vue", operatorV1.Module_LOG},
		{"log bare prefix", "app/log/", operatorV1.Module_LOG},
		{"file page", "app/file/list/index.vue", operatorV1.Module_FILE},

		// 复合前缀：switch 顺序决定归入 system 而非 dict/file/task
		{"system dict page hits system first", "app/system/dict/index.vue", operatorV1.Module_SYSTEM},
		{"system file page hits system first", "app/system/file/index.vue", operatorV1.Module_SYSTEM},

		// 未登记的前缀 / 前缀变体一律 UNSPECIFIED
		{"unknown top dir", "app/unknown/thing/index.vue", operatorV1.Module_MODULE_UNSPECIFIED},
		{"prefix without slash", "app/opm", operatorV1.Module_MODULE_UNSPECIFIED},
		{"prefix with extra letter", "app/opmx/user/index.vue", operatorV1.Module_MODULE_UNSPECIFIED},
		{"dashboard with suffix letter", "dashboardx/index.vue", operatorV1.Module_MODULE_UNSPECIFIED},
		{"case sensitive", "App/OPM/user/index.vue", operatorV1.Module_MODULE_UNSPECIFIED},
		{"leading space not trimmed", " app/opm/user/index.vue", operatorV1.Module_MODULE_UNSPECIFIED},
		{"plain text", "just-a-name", operatorV1.Module_MODULE_UNSPECIFIED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComponentToModule(tc.component))
		})
	}
}

// TestDefaultMenusModuleBackfillInvariant verifies seed menu categories.
func TestDefaultMenusModuleBackfillInvariant(t *testing.T) {
	for _, menu := range DefaultMenus {
		component := menu.GetComponent()
		if component == "" || component == "BasicLayout" {
			continue
		}
		assert.NotEqual(t, operatorV1.Module_MODULE_UNSPECIFIED, ComponentToModule(component), "unclassified seed menu: %s", component)
	}
}
