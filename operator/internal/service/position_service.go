package service

import (
	"context"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/aggregator"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type PositionService struct {
	operatorV1.UnimplementedPositionServiceServer
	log *bLogger.Helper

	positionRepo *data.PositionRepo
	orgUnitRepo  *data.OrgUnitRepo
}

func NewPositionService(
	ctx *bootstrap.Context,
	positionRepo *data.PositionRepo,
	orgUnitRepo *data.OrgUnitRepo,
) *PositionService {
	return &PositionService{
		log:          ctx.NewLoggerHelper("position/service/admin-service"),
		positionRepo: positionRepo,
		orgUnitRepo:  orgUnitRepo,
	}
}

func (s *PositionService) extractRelationIDs(
	positions []*operatorV1.Position,
	orgUnitSet aggregator.ResourceMap[uint32, *operatorV1.OrgUnit],
) {
	for _, p := range positions {
		if p.GetOrgUnitId() > 0 {
			orgUnitSet[p.GetOrgUnitId()] = nil
		}
	}
}

func (s *PositionService) fetchRelationInfo(
	ctx context.Context,
	orgUnitSet aggregator.ResourceMap[uint32, *operatorV1.OrgUnit],
) error {
	if len(orgUnitSet) > 0 {
		orgUnitIds := make([]uint32, 0, len(orgUnitSet))
		for id := range orgUnitSet {
			orgUnitIds = append(orgUnitIds, id)
		}

		orgUnits, err := s.orgUnitRepo.ListOrgUnitsByIds(ctx, orgUnitIds)
		if err != nil {
			s.log.Errorf(context.Background(), "query orgUnits err: %v", err)
			return err
		}

		for _, orgUnit := range orgUnits {
			orgUnitSet[orgUnit.GetId()] = orgUnit
		}
	}

	return nil
}

func (s *PositionService) bindRelations(
	positions []*operatorV1.Position,
	orgUnitSet aggregator.ResourceMap[uint32, *operatorV1.OrgUnit],
) {
	aggregator.Populate(
		positions,
		orgUnitSet,
		func(ou *operatorV1.Position) uint32 { return ou.GetOrgUnitId() },
		func(ou *operatorV1.Position, org *operatorV1.OrgUnit) {
			ou.OrgUnitName = org.Name
		},
	)
}

func (s *PositionService) enrichRelations(ctx context.Context, positions []*operatorV1.Position) error {
	var orgUnitSet = make(aggregator.ResourceMap[uint32, *operatorV1.OrgUnit])
	s.extractRelationIDs(positions, orgUnitSet)
	if err := s.fetchRelationInfo(ctx, orgUnitSet); err != nil {
		return err
	}
	s.bindRelations(positions, orgUnitSet)
	return nil
}

func (s *PositionService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPositionResponse, error) {
	resp, err := s.positionRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	_ = s.enrichRelations(ctx, resp.Items)

	return resp, nil
}

func (s *PositionService) Count(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.CountPositionResponse, error) {
	count, err := s.positionRepo.Count(ctx, req)
	if err != nil {
		return nil, err
	}

	return &operatorV1.CountPositionResponse{
		Count: uint64(count),
	}, nil
}

func (s *PositionService) Get(ctx context.Context, req *operatorV1.GetPositionRequest) (*operatorV1.Position, error) {
	resp, err := s.positionRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	fakeItems := []*operatorV1.Position{resp}
	_ = s.enrichRelations(ctx, fakeItems)

	return resp, nil
}

func (s *PositionService) Create(ctx context.Context, req *operatorV1.CreatePositionRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.positionRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PositionService) Update(ctx context.Context, req *operatorV1.UpdatePositionRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.positionRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PositionService) Delete(ctx context.Context, req *operatorV1.DeletePositionRequest) (*emptypb.Empty, error) {
	if err := s.positionRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
