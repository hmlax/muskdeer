package logging

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/tx7do/go-utils/auditutil"
	"github.com/tx7do/go-utils/trans"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/tx7do/go-crud/viewer"
)

// ApiAuditLogMiddleware records API requests using the configured audit writer.
type ApiAuditLogMiddleware struct {
	op *options
}

// NewApiAuditLogMiddleware borrows the configured audit options.
func NewApiAuditLogMiddleware(op *options) *ApiAuditLogMiddleware {
	return &ApiAuditLogMiddleware{
		op: op,
	}
}

func (a *ApiAuditLogMiddleware) Name() string {
	return "ApiAuditLogMiddleware"
}

func (a *ApiAuditLogMiddleware) Handle(ctx context.Context, htr HTTPTransport, middleErr error, latencyMs int64) {
	// 登录类操作（含 MFA 二次验证）由 LoginAuditLog 负责审计，这里跳过避免重复记录。
	for _, op := range a.op.loginOperations {
		if htr.Operation() == op {
			return
		}
	}

	apiAuditLog := &operatorV1.ApiAuditLog{}

	clientIp := auditutil.ClientRealIP(htr.Request())
	referer, _ := url.QueryUnescape(htr.RequestHeader().Get(HeaderKeyReferer))
	requestUri, _ := url.QueryUnescape(htr.Request().RequestURI)
	bodyBytes, _ := io.ReadAll(htr.Request().Body)

	apiAuditLog.HttpMethod = trans.Ptr(htr.Request().Method)
	apiAuditLog.ApiOperation = trans.Ptr(htr.Operation())
	apiAuditLog.Path = trans.Ptr(htr.PathTemplate())
	apiAuditLog.Referer = trans.Ptr(referer)
	apiAuditLog.IpAddress = trans.Ptr(clientIp)
	apiAuditLog.RequestId = trans.Ptr(auditutil.RequestID(htr.Request()))
	apiAuditLog.RequestUri = trans.Ptr(requestUri)
	apiAuditLog.RequestBody = trans.Ptr(string(bodyBytes))

	ut := extractAuthToken(htr)
	if ut != nil {
		apiAuditLog.UserId = trans.Ptr(ut.UserId)
		apiAuditLog.TenantId = ut.TenantId
		apiAuditLog.Username = ut.Username
	}

	// 地理位置
	apiAuditLog.GeoLocation = fillGeoLocation(clientIp)

	// 用户设备信息
	apiAuditLog.DeviceInfo = fillDeviceInfo(htr, ut)

	// 获取错误码和是否成功
	statusCode, reason, success := getStatusCode(middleErr)

	apiAuditLog.LatencyMs = trans.Ptr(uint32(latencyMs))
	apiAuditLog.StatusCode = trans.Ptr(statusCode)
	apiAuditLog.Reason = trans.Ptr(reason)
	apiAuditLog.Success = trans.Ptr(success)

	// 计算哈希和签名
	apiAuditLog.LogHash = trans.Ptr(auditutil.HashLog(apiAuditLog))
	signature, signErr := auditutil.SignLogContent(a.op.ecPrivateKey, apiAuditLog.GetTenantId(), apiAuditLog.GetUserId(), apiAuditLog.GetCreatedAt(), apiAuditLog.GetLogHash())
	if signErr != nil {
		fmt.Printf("sign log content failed: %v\n", signErr)
	}
	apiAuditLog.Signature = signature

	// 写入日志
	if a.op.writeApiLogFunc != nil {
		ctx = viewer.WithSystemContext(ctx)
		_ = a.op.writeApiLogFunc(ctx, apiAuditLog)
	}
}
