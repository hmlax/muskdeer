// 本文件补 policy_eval_logging_engine 的白盒测试：
//
//	newEvalLoggingEngine 的 nil 守卫与包装分支；
//	describe 的 effect_details 各分支（允许/拒绝/引擎错误、有无 project）；
//	TraceIDFromContext / ClientIPFromContext 在无 transport、无 HTTP 请求元数据
//	  假件下的短路返回；
//	evaluationContextJSON 的 project 与 rec 空值分支。
//
// IsAuthorized 主体与 resolvePolicyCached 的查库路径依赖
// policyLogRepository 的可用实例（跨包白盒装配受限），由数据层测试覆盖。
package mw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authzEngine "github.com/tx7do/kratos-authz/engine"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// stubAuthzEngine 实现 authzEngine.Authorizer 的桩：只关心 Name()。
type stubAuthzEngine struct{ name string }

func (s *stubAuthzEngine) Name() string { return s.name }

func (s *stubAuthzEngine) IsAuthorized(context.Context, authzEngine.Subject, authzEngine.Action, authzEngine.Resource, authzEngine.Project) (bool, error) {
	return true, nil
}

func (s *stubAuthzEngine) ProjectsAuthorized(context.Context, authzEngine.Subjects, authzEngine.Action, authzEngine.Resource, authzEngine.Projects) (authzEngine.Projects, error) {
	return nil, nil
}

func (s *stubAuthzEngine) FilterAuthorizedPairs(context.Context, authzEngine.Subjects, authzEngine.Pairs) (authzEngine.Pairs, error) {
	return nil, nil
}

func (s *stubAuthzEngine) FilterAuthorizedProjects(context.Context, authzEngine.Subjects) (authzEngine.Projects, error) {
	return nil, nil
}

// TestNewEvalLoggingEngine_NilGuards inner/repo 任一为 nil 都不得包装：
// 返回 nil 或原样返回 inner；两者齐备时才产出 *evalLoggingEngine。
func TestNewEvalLoggingEngine_NilGuards(t *testing.T) {
	inner := &stubAuthzEngine{name: "stub"}

	assert.Nil(t, newEvalLoggingEngine(nil, nil))
	assert.Nil(t, newEvalLoggingEngine(nil, &policyRepo{}))

	same := newEvalLoggingEngine(inner, nil)
	assert.Same(t, inner, same, "repo 为 nil 时必须原样返回 inner")

	wrapped := newEvalLoggingEngine(inner, &policyRepo{})
	_, ok := wrapped.(*evalLoggingEngine)
	assert.True(t, ok, "inner 与 repo 齐备时应产出包装引擎")
}

// TestEvalLoggingEngine_Describe describe 的 effect_details 分支：
// 允许/拒绝/引擎错误三态，以及 project 字段的有无。
func TestEvalLoggingEngine_Describe(t *testing.T) {
	e := &evalLoggingEngine{Authorizer: &stubAuthzEngine{name: "stub-engine"}}

	for name, tc := range map[string]struct {
		project  authzEngine.Project
		err      error
		allowed  bool
		contains []string
		missing  []string
	}{
		"允许":       {"", nil, true, []string{"engine=stub-engine subject=role-x", "latency=", "; allowed"}, []string{"project=", "error:", "denied"}},
		"拒绝":       {"", nil, false, []string{"engine=stub-engine subject=role-x", "; denied"}, []string{"project=", "error:", "; allowed"}},
		"引擎错误":     {"", errors.New("boom"), true, []string{"engine=stub-engine subject=role-x", "; error: boom"}, []string{"project=", "; allowed", "denied"}},
		"带project": {"proj-1", nil, true, []string{"engine=stub-engine subject=role-x", " project=proj-1", "; allowed"}, []string{"error:", "denied"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := e.describe(time.Now(), "role-x", tc.project, tc.err, tc.allowed)
			for _, want := range tc.contains {
				assert.Contains(t, got, want)
			}
			for _, ban := range tc.missing {
				assert.NotContains(t, got, ban)
			}
		})
	}
}

// TestTraceAndClientIP_NonHttpTransport 无 transport 或无 HTTP 请求元数据 的
// 假件下，trace/clientIP 提取必须短路返回空串。
func TestTraceAndClientIP_NonHttpTransport(t *testing.T) {
	require.Equal(t, "", TraceIDFromContext(context.Background()))
	require.Equal(t, "", ClientIPFromContext(context.Background()))

	ctx := httpmeta.NewContext(context.Background(), &httpmeta.Exchange{})
	require.Equal(t, "", TraceIDFromContext(ctx))
	require.Equal(t, "", ClientIPFromContext(ctx))
}

// TestEvaluationContextJSON_Branches 快照 JSON 的 project 与 rec 空值分支：
// project 非空时必须带 project 键；rec 为 nil 或字段为 nil 时不得带 userId/tenantId。
func TestEvaluationContextJSON_Branches(t *testing.T) {
	t.Run("project非空", func(t *testing.T) {
		got := evaluationContextJSON("eng", "role", "GET", "/r", "proj-9", nil)
		require.Contains(t, got, `"project":"proj-9"`)
		require.Contains(t, got, `"engine":"eng"`)
	})
	t.Run("rec为nil", func(t *testing.T) {
		got := evaluationContextJSON("eng", "role", "GET", "/r", "", nil)
		require.NotContains(t, got, "userId")
		require.NotContains(t, got, "tenantId")
		require.NotContains(t, got, "project")
	})
	t.Run("rec字段为nil", func(t *testing.T) {
		got := evaluationContextJSON("eng", "role", "GET", "/r", "", &operatorV1.PolicyEvaluationLog{})
		require.NotContains(t, got, "userId")
		require.NotContains(t, got, "tenantId")
	})
}
