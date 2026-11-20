package service

import (
	"testing"
)

// 审计日志导出列必须与列表页展示列一致（"导出的就是页面看到的"）。
// 策略评估日志的 logHash/signature 是完整性校验字段，页面不展示，不进导出。
func TestAuditExportColumnHeaders(t *testing.T) {
	cases := []struct {
		name string
		cols []string
		want []string
	}{
		{"policy_evaluation", headersOf(policyEvaluationExportColumns()), []string{
			"id", "createdAt", "tenantId", "userId", "membershipId", "permissionId",
			"policyId", "requestPath", "requestMethod", "result", "effectDetails",
			"scopeSql", "ipAddress", "traceId", "evaluationContext",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.cols) != len(c.want) {
				t.Fatalf("column count = %d, want %d", len(c.cols), len(c.want))
			}
			set := map[string]bool{}
			for _, h := range c.cols {
				set[h] = true
			}
			for _, w := range c.want {
				if !set[w] {
					t.Errorf("missing expected column header %q", w)
				}
			}
		})
	}
}

func headersOf[T any](columns []exportColumn[T]) []string {
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, column.header)
	}
	return headers
}
