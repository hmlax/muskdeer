package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/authorizer"
	"google.golang.org/protobuf/types/known/emptypb"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/service"
)

// GatewayService exposes typed data operations needed by the HTTP boundary.
type GatewayService struct {
	operatorV1.UnimplementedGatewayServiceServer
	Policies        authorizer.Provider
	Access          *data.GatewayAccessRepo
	APIAudit        *data.ApiAuditLogRepo
	LoginAudit      *data.LoginAuditLogRepo
	OperationAudit  *data.OperationAuditLogRepo
	PermissionAudit *data.PermissionAuditLogRepo
	DataAccessAudit *data.DataAccessAuditLogRepo
	PolicyAudit     *data.PolicyEvaluationLogRepo
	AuditExport     *service.AuditExportService

	Files *service.FileTransferService
}

func (s *GatewayService) GetPolicies(ctx context.Context, _ *emptypb.Empty) (*operatorV1.PoliciesResponse, error) {
	policies, err := s.Policies.ProvidePolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := &operatorV1.PoliciesResponse{Policies: make([]*operatorV1.RolePolicy, 0, len(policies))}
	for role, permissions := range policies {
		entry := &operatorV1.RolePolicy{Role: role}
		for _, p := range permissions {
			entry.Permissions = append(entry.Permissions, &operatorV1.GatewayPermission{
				Domain: p.Domain,
				Path:   p.Path,
				Method: p.Method,
			})
		}
		out.Policies = append(out.Policies, entry)
	}
	return out, nil
}
func (s *GatewayService) GetTenantAccessData(ctx context.Context, req *operatorV1.TenantAccessRequest) (*operatorV1.TenantAccessData, error) {
	return s.Access.Get(ctx, req)
}

func (s *GatewayService) ResolveTenantCode(ctx context.Context, req *operatorV1.ResolveTenantCodeRequest) (*operatorV1.ResolveTenantCodeResponse, error) {
	return s.Access.ResolveTenantCode(ctx, req.GetCode())
}
func (s *GatewayService) WriteAudit(ctx context.Context, req *operatorV1.AuditRecord) (*emptypb.Empty, error) {
	ctx = viewer.WithSystemContext(ctx)
	var err error
	switch record := req.GetRecord().(type) {
	case *operatorV1.AuditRecord_Api:
		err = s.APIAudit.Create(ctx, &operatorV1.CreateApiAuditLogRequest{Data: record.Api})
	case *operatorV1.AuditRecord_Login:
		err = s.LoginAudit.Create(ctx, &operatorV1.CreateLoginAuditLogRequest{Data: record.Login})
	case *operatorV1.AuditRecord_Operation:
		err = s.OperationAudit.Create(ctx, &operatorV1.CreateOperationAuditLogRequest{Data: record.Operation})
	case *operatorV1.AuditRecord_Permission:
		err = s.PermissionAudit.Create(ctx, &operatorV1.CreatePermissionAuditLogRequest{Data: record.Permission})
	case *operatorV1.AuditRecord_DataAccess:
		err = s.DataAccessAudit.Create(ctx, &operatorV1.CreateDataAccessAuditLogRequest{Data: record.DataAccess})
	case *operatorV1.AuditRecord_PolicyEvaluation:
		err = s.PolicyAudit.Create(ctx, &operatorV1.CreatePolicyEvaluationLogRequest{Data: record.PolicyEvaluation})
	default:
		return nil, fmt.Errorf("audit record is required")
	}
	return &emptypb.Empty{}, err
}
func (s *GatewayService) ResolvePolicy(ctx context.Context, req *operatorV1.ResolvePolicyRequest) (*operatorV1.ResolvePolicyResponse, error) {
	permission, policy := s.PolicyAudit.ResolvePermissionPolicyByRoute(viewer.WithSystemContext(ctx), req.GetSubject(), req.GetPath(), req.GetMethod())
	return &operatorV1.ResolvePolicyResponse{
		PermissionId: permission,
		PolicyId:     policy,
	}, nil
}

type fileWriter struct {
	bytes.Buffer
	header http.Header
	status int
}

func (w *fileWriter) Header() http.Header { return w.header }
func (w *fileWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *fileWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.Buffer.Write(data)
}
func (w *fileWriter) response() *operatorV1.FileResponse {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	return &operatorV1.FileResponse{
		Content:      w.Bytes(),
		ContentType:  w.header.Get("Content-Type"),
		Disposition:  w.header.Get("Content-Disposition"),
		Status:       int32(status),
		CacheControl: w.header.Get("Cache-Control"),
	}
}
func (s *GatewayService) Export(ctx context.Context, req *operatorV1.ExportRequest) (*operatorV1.FileResponse, error) {
	q := url.Values{"type": {req.GetType()}, "format": {req.GetFormat()}, "maxRows": {strconv.FormatUint(uint64(req.GetMaxRows()), 10)}, "query": {req.GetQuery()}}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://admin/export?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	w := &fileWriter{header: make(http.Header)}
	switch req.GetResource() {
	case operatorV1.ExportRequest_AUDIT:
		err = s.AuditExport.ServeExport(w, r)
	default:
		return nil, fmt.Errorf("unknown export resource")
	}
	if err != nil {
		return nil, err
	}
	return w.response(), nil
}
func (s *GatewayService) ReadMedia(ctx context.Context, req *operatorV1.MediaRequest) (*operatorV1.FileResponse, error) {
	q := url.Values{"path": {req.GetPath()}, "expires": {strconv.FormatInt(req.GetExpires(), 10)}, "sig": {req.GetSignature()}}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://admin/image?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	w := &fileWriter{header: make(http.Header)}
	if err := s.Files.ServeImageHandler(w, r); err != nil {
		return nil, err
	}
	return w.response(), nil
}
