package mw

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"time"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	authzEngine "github.com/tx7do/kratos-authz/engine"
	"go.opentelemetry.io/otel/trace"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// evalLoggingEngine 包装 authz 引擎，把每次 IsAuthorized 评估判定落
// PolicyEvaluationLog（sys_policy_evaluation_logs）。判定信息与日志 schema 对应：
// subject=角色码、action=HTTP 方法、resource=路径模板（由 auth 中间件构建 claims）。
// 落库失败只吞错，不影响鉴权主流程。多角色请求每个角色评估各落一行。
type policyLogRepository interface {
	Create(context.Context, *operatorV1.CreatePolicyEvaluationLogRequest) error
	ResolvePermissionPolicyByRoute(context.Context, string, string, string) (uint32, uint32)
}

type evalLoggingEngine struct {
	authzEngine.Authorizer
	repo policyLogRepository

	// resolveCache 缓存 (subject|path|method)→(权限点ID,策略ID) 反查结果，
	// 避免每次评估（每角色每请求）都打 DB；权限数据变更最多 60s 延迟可见。
	resolveMu    sync.Mutex
	resolveCache map[string]resolveResult
}

type resolveResult struct {
	permissionID uint32
	policyID     uint32
	expiresAt    time.Time
}

const resolveTTL = 60 * time.Second

// newEvalLoggingEngine 包装引擎；inner 或 repo 为 nil 时原样返回（不埋点）。
func newEvalLoggingEngine(inner authzEngine.Authorizer, repo policyLogRepository) authzEngine.Authorizer {
	if inner == nil || repo == nil {
		return inner
	}
	return &evalLoggingEngine{
		Authorizer:   inner,
		repo:         repo,
		resolveCache: make(map[string]resolveResult),
	}
}

func (e *evalLoggingEngine) IsAuthorized(ctx context.Context, subject authzEngine.Subject, action authzEngine.Action, resource authzEngine.Resource, project authzEngine.Project) (bool, error) {
	start := time.Now()
	allowed, err := e.Authorizer.IsAuthorized(ctx, subject, action, resource, project)

	rec := &operatorV1.PolicyEvaluationLog{
		RequestPath:   trans.Ptr(string(resource)),
		RequestMethod: trans.Ptr(string(action)),
		Result:        trans.Ptr(allowed && err == nil),
		EffectDetails: trans.Ptr(e.describe(start, subject, project, err, allowed)),
	}

	if ut, utErr := auth.FromContext(ctx); utErr == nil && ut != nil {
		rec.UserId = trans.Ptr(ut.UserId)
		rec.TenantId = ut.TenantId
	}
	if ip := ClientIPFromContext(ctx); ip != "" {
		rec.IpAddress = trans.Ptr(ip)
	}
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		rec.TraceId = trans.Ptr(traceID)
	}
	if ctxJSON := evaluationContextJSON(e.Name(), subject, action, resource, project, rec); ctxJSON != "" {
		rec.EvaluationContext = trans.Ptr(ctxJSON)
	}
	// 反查评估命中的权限点与挂接策略（角色码+路径+方法 → permission → policy），
	// 尽力而为：API 未登记/无策略行时对应字段留空。
	if pid, pol := e.resolvePolicyCached(ctx, string(subject), string(resource), string(action)); pid != 0 || pol != 0 {
		if pid != 0 {
			rec.PermissionId = trans.Ptr(pid)
		}
		if pol != 0 {
			rec.PolicyId = trans.Ptr(pol)
		}
	}

	// SystemContext：评估发生在 authz 阶段，无租户 viewer 上下文，落库需系统视角。
	_ = e.repo.Create(viewer.WithSystemContext(ctx), &operatorV1.CreatePolicyEvaluationLogRequest{Data: rec})

	return allowed, err
}

// resolvePolicyCached 带 TTL 缓存的反查；查询走 SystemContext（评估阶段无租户 viewer）。
func (e *evalLoggingEngine) resolvePolicyCached(ctx context.Context, subject, path, method string) (permissionID, policyID uint32) {
	if e.repo == nil {
		return 0, 0
	}
	key := subject + "|" + path + "|" + strings.ToUpper(method)

	e.resolveMu.Lock()
	if hit, ok := e.resolveCache[key]; ok && time.Now().Before(hit.expiresAt) {
		e.resolveMu.Unlock()
		return hit.permissionID, hit.policyID
	}
	e.resolveMu.Unlock()

	pid, pol := e.repo.ResolvePermissionPolicyByRoute(viewer.WithSystemContext(ctx), subject, path, method)

	e.resolveMu.Lock()
	e.resolveCache[key] = resolveResult{
		permissionID: pid,
		policyID:     pol,
		expiresAt:    time.Now().Add(resolveTTL),
	}
	e.resolveMu.Unlock()
	return pid, pol
}

// TraceIDFromContext uses the active server span, then legacy request headers.
func TraceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	htr, ok := httpmeta.FromContext(ctx)
	if !ok {
		return ""
	}
	if htr.Request() == nil {
		return ""
	}
	return traceIDFromHeader(htr.Request().Header)
}

// traceIDFromHeader 从请求头解析追踪ID（纯函数，便于单测）。
func traceIDFromHeader(header nethttp.Header) string {
	if tp := strings.TrimSpace(header.Get("traceparent")); tp != "" {
		// 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
		parts := strings.Split(tp, "-")
		if len(parts) >= 2 && len(parts[1]) == 32 {
			return parts[1]
		}
	}
	if rid := strings.TrimSpace(header.Get("X-Request-Id")); rid != "" {
		return rid
	}
	return ""
}

// evaluationContextJSON 生成决策上下文快照（JSON）：引擎、角色、动作、资源、
// 项目与操作者归属。 proto 语义为"用户属性、环境属性"的评估输入留档。
func evaluationContextJSON(engine string, subject authzEngine.Subject, action authzEngine.Action, resource authzEngine.Resource, project authzEngine.Project, rec *operatorV1.PolicyEvaluationLog) string {
	snapshot := map[string]any{
		"engine":   engine,
		"subject":  string(subject),
		"action":   string(action),
		"resource": string(resource),
	}
	if project != "" {
		snapshot["project"] = string(project)
	}
	if rec != nil {
		if rec.UserId != nil {
			snapshot["userId"] = rec.GetUserId()
		}
		if rec.TenantId != nil {
			snapshot["tenantId"] = rec.GetTenantId()
		}
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(b)
}

// describe 生成 effect_details：引擎、角色、项目、耗时，err/拒绝时附原因。
func (e *evalLoggingEngine) describe(start time.Time, subject authzEngine.Subject, project authzEngine.Project, err error, allowed bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "engine=%s subject=%s", e.Name(), subject)
	if project != "" {
		fmt.Fprintf(&sb, " project=%s", project)
	}
	fmt.Fprintf(&sb, " latency=%dms", time.Since(start).Milliseconds())
	switch {
	case err != nil:
		fmt.Fprintf(&sb, "; error: %v", err)
	case !allowed:
		sb.WriteString("; denied")
	default:
		sb.WriteString("; allowed")
	}
	return sb.String()
}

// ClientIPFromContext 从 transport 尽力取客户端 IP（代理头优先）。
func ClientIPFromContext(ctx context.Context) string {
	htr, ok := httpmeta.FromContext(ctx)
	if !ok {
		return ""
	}
	if htr.Request() == nil {
		return ""
	}
	req := htr.Request()
	if ip := strings.TrimSpace(req.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return host
	}
	return req.RemoteAddr
}
