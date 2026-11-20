package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/transport"
	"github.com/tx7do/go-utils/auditutil"

	"github.com/hmlax/muskdeer/deer/management/audit"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// HTTPTransport describes the facts required for auditing, independent of the router.
type HTTPTransport interface {
	Operation() string
	PathTemplate() string
	Request() *http.Request
	RequestHeader() transport.Header
	ReplyHeader() transport.Header
}

// Recorder is shared by native Hertz middleware and legacy RPC middleware.
type Recorder struct {
	login      *LoginAuditLogMiddleware
	api        *ApiAuditLogMiddleware
	operation  *OperationAuditLogMiddleware
	permission *PermissionAuditLogMiddleware
	data       *DataAccessAuditLogMiddleware
}

func NewRecorder(opts ...Option) (*Recorder, error) {
	op := options{
		loginOperations: []string{operatorV1.OperationAuthenticationServiceLogin, operatorV1.OperationMfaServiceVerifyMFAChallenge},
		logoutOperation: operatorV1.OperationAuthenticationServiceLogout,
	}
	for _, apply := range opts {
		apply(&op)
	}
	if op.ecPrivateKey == nil || op.ecPublicKey == nil {
		var err error
		op.ecPrivateKey, op.ecPublicKey, err = auditutil.GenerateECDSAKeyPair()
		if err != nil {
			return nil, fmt.Errorf("initialize audit signing: %w", err)
		}
	}
	return &Recorder{
		login:      NewLoginAuditLogMiddleware(&op),
		api:        NewApiAuditLogMiddleware(&op),
		operation:  NewOperationAuditLogMiddleware(&op),
		permission: NewPermissionAuditLogMiddleware(&op),
		data:       NewDataAccessAuditLogMiddleware(&op),
	}, nil
}

func (r *Recorder) Begin(ctx context.Context, exchange HTTPTransport) context.Context {
	events := make([]audit.AuditEvent, 0)
	ctx = context.WithValue(ctx, audit.AccumulatorKey(), &events)
	return snapshotWriteBody(ctx, exchange.Request())
}

func (r *Recorder) Finish(ctx context.Context, exchange HTTPTransport, err error, elapsed time.Duration) {
	ctx = context.WithValue(ctx, audit.SinkKey(), true)
	r.login.Handle(ctx, exchange, err)
	// Native binding reads Hertz's body rather than this metadata reader. Keep
	// the legacy consumed-body behavior so credentials and multipart bytes never
	// enter API audit storage; target-name auditing uses the bounded snapshot.
	if request := exchange.Request(); request != nil {
		request.Body = io.NopCloser(bytes.NewReader(nil))
	}
	r.api.Handle(ctx, exchange, err, elapsed.Milliseconds())
	r.operation.Handle(ctx, exchange, err, elapsed.Milliseconds())
	r.permission.Handle(ctx, exchange, err, elapsed.Milliseconds())
	r.data.Handle(ctx, exchange, err, elapsed.Milliseconds())
}
