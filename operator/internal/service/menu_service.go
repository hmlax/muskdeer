package service

import (
	"context"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/tx7do/go-crud/viewer"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type MenuService struct {
	operatorV1.UnimplementedMenuServiceServer
	log *bLogger.Helper

	menuRepo *data.MenuRepo
}

func NewMenuService(ctx *bootstrap.Context, menuRepo *data.MenuRepo) *MenuService {
	svc := &MenuService{
		log:      ctx.NewLoggerHelper("menu/service/admin-service"),
		menuRepo: menuRepo,
	}

	svc.init()

	return svc
}

func (s *MenuService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.menuRepo.Count(ctx, nil); count == 0 {
		_ = s.createDefaultMenus(ctx)
	}

	// 种子是带显式 ID 插入的，PG 的 identity 序列不会因此前进；不对齐的话
	// 「新建菜单」与菜单同步的新增分支必撞 sys_menus_pkey（HTTP 500）。
	// 放在 count==0 守卫之外：已部署实例的序列同样落后，需要每次启动自愈。
	if err := s.menuRepo.AlignIdentitySequence(ctx); err != nil {
		s.log.Errorf(ctx, "菜单 id 序列对齐失败: %v", err)
	}
}

func (s *MenuService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListMenuResponse, error) {
	ret, err := s.menuRepo.List(ctx, req, false)
	if err != nil {

		return nil, err
	}

	return ret, nil
}

func (s *MenuService) Get(ctx context.Context, req *operatorV1.GetMenuRequest) (*operatorV1.Menu, error) {
	ret, err := s.menuRepo.Get(ctx, req)
	if err != nil {

		return nil, err
	}

	return ret, nil
}

func (s *MenuService) Create(ctx context.Context, req *operatorV1.CreateMenuRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.menuRepo.Create(ctx, req); err != nil {

		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *MenuService) Update(ctx context.Context, req *operatorV1.UpdateMenuRequest) (*emptypb.Empty, error) {
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

	if err = s.menuRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *MenuService) Delete(ctx context.Context, req *operatorV1.DeleteMenuRequest) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.OperatorId = trans.Ptr(operator.UserId)

	if err := s.menuRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// SyncMenus 同步菜单：将前端推送的路由树按模式合并进数据库。
// REPLACE（缺省）= 清空重建（菜单 ID 全变，角色授权失效）；MERGE = 按全路径增量合并（保留 ID 与角色授权）。
// 两模式均在单个事务内执行（见 MenuRepo.SyncMenus）。
func (s *MenuService) SyncMenus(ctx context.Context, req *operatorV1.SyncMenusRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	count, err := s.menuRepo.SyncMenus(ctx, req.GetItems(), req.GetMode(), operator.UserId)
	if err != nil {
		return nil, err
	}

	s.log.Infof(ctx, "sync menus success, mode: %s, total: %d", req.GetMode().String(), count)

	return &emptypb.Empty{}, nil
}

func (s *MenuService) createDefaultMenus(ctx context.Context) error {
	for _, m := range constants.DefaultMenus {
		m.Module = trans.Ptr(constants.ComponentToModule(m.GetComponent()))
		if err := s.menuRepo.Create(ctx, &operatorV1.CreateMenuRequest{Data: m}); err != nil {
			s.log.Errorf(ctx, "create default menu err: %v", err)
			return err
		}
	}
	return nil
}
