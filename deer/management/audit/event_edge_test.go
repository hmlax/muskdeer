// 本文件补 deer/management/audit 的事件上下文边角分支：
//
//	event.go 四个 context 辅助函数（AccumulatorKey/SinkKey/FromContext/IsSinking）
//	此前零覆盖——它们是采集管道（wrapper→accumulator→落库侧防递归）的键位约定；
package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func contextBackground() context.Context { return context.Background() }
func contextWithValue(ctx context.Context, k, v any) context.Context {
	return context.WithValue(ctx, k, v)
}

// TestEventContextKeys accumulator/sink 键位约定与取回语义：
// 值经 context.WithValue 植入后 FromContext/IsSinking 必须按原语义取回；
// 无植入时 FromContext 不命中、IsSinking 为 false。
func TestEventContextKeys(t *testing.T) {
	require.NotNil(t, AccumulatorKey())
	require.NotNil(t, SinkKey())

	events := make([]AuditEvent, 0)
	ctx := contextWithValue(contextBackground(), AccumulatorKey(), &events)
	got, ok := FromContext(ctx)
	require.True(t, ok, "植入 accumulator 后 FromContext 应命中")
	require.Same(t, &events, got)

	_, ok = FromContext(contextBackground())
	require.False(t, ok, "未植入时 FromContext 不得命中")

	require.False(t, IsSinking(contextBackground()), "未植入 sink 标记时 IsSinking 应为 false")
	sinkCtx := contextWithValue(contextBackground(), SinkKey(), true)
	require.True(t, IsSinking(sinkCtx), "植入 sink 标记后 IsSinking 应为 true")
}
