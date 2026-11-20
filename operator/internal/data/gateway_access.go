package data

import (
	"context"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/hmlax/muskdeer/deer/management/portalscope"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"
	"github.com/hmlax/muskdeer/operator/internal/data/store/tenant"
)

type GatewayAccessRepo struct{ client *store.Client }

func NewGatewayAccessRepo(_ *bootstrap.Context, db *store.DBClient) *GatewayAccessRepo {
	return &GatewayAccessRepo{client: db.Client()}
}
func (r *GatewayAccessRepo) ResolveTenantCode(ctx context.Context, code string) (*operatorV1.ResolveTenantCodeResponse, error) {
	row, err := r.client.Tenant.Query().Where(tenant.CodeEQ(code)).Only(viewer.WithSystemContext(ctx))
	if store.IsNotFound(err) {
		return nil, operatorV1.ErrorAdminForbidden(portalscope.ErrorUnavailable)
	}
	if err != nil {
		return nil, err
	}
	return &operatorV1.ResolveTenantCodeResponse{TenantId: row.ID}, nil
}
func (r *GatewayAccessRepo) Get(ctx context.Context, request *operatorV1.TenantAccessRequest) (*operatorV1.TenantAccessData, error) {
	ctx = viewer.WithSystemContext(ctx)
	result := new(operatorV1.TenantAccessData)
	row, err := r.client.Tenant.Query().Where(tenant.IDEQ(request.GetTenantId())).Only(ctx)
	if store.IsNotFound(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.TenantExists = true
	if row.Status != nil {
		value := string(*row.Status)
		result.Status = &value
	}
	if row.ExpiredAt != nil && !row.ExpiredAt.IsZero() {
		value := row.ExpiredAt.Unix()
		result.ExpiresAt = &value
	}
	result.ApiExists, err = r.client.Api.Query().Where(api.PathEQ(request.GetPath()), api.MethodEQ(request.GetMethod())).Exist(ctx)
	return result, err
}
