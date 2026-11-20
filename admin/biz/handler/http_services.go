package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/tx7do/go-utils/crypto"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	"github.com/hmlax/muskdeer/admin/biz/mw"
	"github.com/hmlax/muskdeer/admin/internal/transport"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type ExportHTTPService struct {
	client operatorV1.ExportServiceClient
}

func (s *ExportHTTPService) ExportAuditLogs(ctx context.Context, in *apiV1.AuditExportRequest) (*apiV1.BinaryFileResponse, error) {
	if err := checkExportActor(ctx, true); err != nil {
		return nil, err
	}
	request := toRPCAuditExportRequest(in)
	if err := transport.ValidateRequest(request); err != nil {
		return nil, err
	}
	reply, err := s.client.ExportAuditLogs(ctx, request)
	if err != nil {
		return nil, err
	}
	return toAPIBinaryFileResponse(reply), nil
}

func checkExportActor(ctx context.Context, platformOnly bool) error {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return err
	}
	if platformOnly && !actor.GetIsPlatformAdmin() {
		return operatorV1.ErrorAdminForbidden(mw.ErrorPlatformRequired)
	}
	return nil
}

type MediaHTTPService struct {
	client operatorV1.MediaServiceClient
}

func (s *MediaHTTPService) ReadImage(ctx context.Context, in *apiV1.ReadMediaRequest) (*apiV1.BinaryFileResponse, error) {
	if time.Now().Unix() > in.GetExpires() || !crypto.VerifyData(in.GetPath()+"|"+strconv.FormatInt(in.GetExpires(), 10), in.GetSig()) {
		return nil, operatorV1.ErrorAdminForbidden("invalid or expired media signature")
	}
	request := toRPCReadMediaRequest(in)
	if err := transport.ValidateRequest(request); err != nil {
		return nil, err
	}
	reply, err := s.client.ReadImage(ctx, request)
	if err != nil {
		return nil, err
	}
	return toAPIBinaryFileResponse(reply), nil
}

// NewExportHTTPService forwards typed export requests to operator.
func NewExportHTTPService(client operatorV1.ExportServiceClient) *ExportHTTPService {
	return &ExportHTTPService{client: client}
}

// NewMediaHTTPService forwards signed media requests to operator.
func NewMediaHTTPService(client operatorV1.MediaServiceClient) *MediaHTTPService {
	return &MediaHTTPService{client: client}
}
