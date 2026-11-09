package console

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	portfolio "github.com/hmlax/muskdeer/deer/pb/portfolio/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

type inventory struct {
	service   string
	revisions map[int64]int64
	available bool
}

func (s *Server) inventories(ctx context.Context) []inventory {
	result := []inventory{
		{
			service:   rpcsecurity.ServiceStrategy,
			revisions: s.revisions,
			available: true,
		},
		{service: rpcsecurity.ServiceMarket},
		{service: rpcsecurity.ServiceExecution},
		{service: rpcsecurity.ServicePortfolio},
	}
	var group errgroup.Group
	for index := 1; index < len(result); index++ {
		group.Go(func() error {
			item := &result[index]
			conn, err := s.factory.Client(rpcclient.Target{Service: item.service})
			if err != nil {
				return nil
			}
			request := &common.VenueInstanceInventoryRequest{}
			var reply *common.VenueInstanceInventory
			switch item.service {
			case rpcsecurity.ServiceMarket:
				reply, err = market.NewMarketClient(conn).DescribeInstances(ctx, request)
			case rpcsecurity.ServiceExecution:
				reply, err = execution.NewExecutionClient(conn).DescribeInstances(ctx, request)
			case rpcsecurity.ServicePortfolio:
				reply, err = portfolio.NewPortfolioClient(conn).DescribeInstances(ctx, request)
			}
			if err != nil {
				return nil
			}
			item.available = true
			item.revisions = make(map[int64]int64, len(reply.GetInstances()))
			for _, row := range reply.GetInstances() {
				item.revisions[row.Id] = row.Revision
			}
			return nil
		})
	}
	_ = group.Wait() // Each unavailable service is represented explicitly below.
	return result
}

// ListInstances lists current parameters together with observed startup revisions.
func (s *Server) ListInstances(ctx context.Context, in *pb.InstanceListRequest) (*pb.InstancePage, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	offset, limit, err := pagination(in.GetPage(), in.GetPageSize())
	if err != nil {
		return nil, err
	}
	q := scoped(fresh(ctx, s.db).Model(&model.StrategyInstance{}).Where("delete_at IS NULL"), in.Actor)
	if v := strings.TrimSpace(in.GetSearch()); v != "" {
		q = q.Where("instance_key ILIKE ?", "%"+v+"%")
	}
	if in.GetAccountId() != "" {
		q = q.Where("account_id = ?", in.AccountId)
	}
	if in.GetStrategyType() != "" {
		q = q.Where("template = ?", in.StrategyType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*model.StrategyInstance
	if err := q.Order("id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	out := &pb.InstancePage{Total: uint64(total)}
	versions := s.inventories(ctx)
	for _, row := range rows {
		item, err := s.instance(ctx, row, versions)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (s *Server) row(ctx context.Context, actor *pb.Actor, id int64) (*model.StrategyInstance, error) {
	if err := accountScope(actor); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	var row model.StrategyInstance
	err := scoped(fresh(ctx, s.db).Model(&model.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", id), actor).Take(&row).Error
	if err != nil {
		return nil, s.fail(ctx, err)
	}
	return &row, nil
}

// GetInstance returns one instance only within the actor account scope.
func (s *Server) GetInstance(ctx context.Context, in *pb.InstanceRequest) (*pb.Instance, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	row, err := s.row(ctx, in.GetActor(), in.GetId())
	if err != nil {
		return nil, err
	}
	return s.instance(ctx, row, s.inventories(ctx))
}

func (s *Server) instance(ctx context.Context, row *model.StrategyInstance, versions []inventory) (*pb.Instance, error) {
	var product commonmodel.Instrument
	if err := fresh(ctx, s.db).Model(&commonmodel.Instrument{}).Where("id = ?", row.InstrumentId).Take(&product).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	out := &pb.Instance{
		Id:               row.Id,
		InstanceKey:      row.InstanceKey,
		StrategyType:     row.Template,
		Implementation:   row.Implementation,
		TemplateId:       row.TemplateId,
		TemplateRevision: row.TemplateRevision,
		AccountId:        row.AccountId,
		InstrumentId:     row.InstrumentId,
		Symbol:           product.Symbol,
		MarketDataSource: row.MarketDataSource,
		DesiredRunning:   row.DesiredRunning,
		Revision:         row.Revision,
		ControlVersion:   row.RequestedConfigVersion,
		Parameters:       parametersToWire(row.Parameters),
		CreatedAtMs:      row.CreateAt,
		UpdatedAtMs:      row.UpdateAt,
		Archived:         row.DeleteAt != nil,
	}
	for _, v := range versions {
		revision, found := v.revisions[row.Id]
		available := v.available && found
		out.Runtimes = append(out.Runtimes, &pb.RuntimeVersion{
			Service:   v.service,
			Revision:  revision,
			Available: available,
		})
		out.RestartRequired = out.RestartRequired || !available || revision != row.Revision
	}
	return out, nil
}

func (s *Server) manage(ctx context.Context, actor *pb.Actor, command instanceconfig.ManagementWrite) (*pb.Instance, error) {
	if err := s.authorize(ctx, actor, true); err != nil {
		return nil, err
	}
	if err := accountScope(actor); err != nil {
		return nil, err
	}
	command.Subject, command.RequestID = actor.Subject, actor.RequestId
	if actor.TenantId > 0 {
		command.Accounts = actor.AllowedAccountIds
	}
	row, err := instanceconfig.ManageConfigured(ctx, s.db, command)
	if err != nil {
		switch {
		case errors.Is(err, instanceconfig.ErrConflict):
			return nil, status.Error(codes.Aborted, err.Error())
		case errors.Is(err, instanceconfig.ErrInvalid):
			return nil, status.Error(codes.InvalidArgument, messageInvalid)
		case errors.Is(err, gorm.ErrDuplicatedKey):
			return nil, status.Error(codes.AlreadyExists, messageInvalid)
		default:
			return nil, s.fail(ctx, err)
		}
	}
	return s.instance(ctx, row, s.inventories(ctx))
}

// CreateInstance saves a stopped instance with a complete typed parameter set.
func (s *Server) CreateInstance(ctx context.Context, in *pb.CreateInstanceRequest) (*pb.Instance, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	p, err := parametersFromWire(in.GetParameters())
	if err != nil {
		return nil, err
	}
	return s.manage(ctx, in.Actor, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Input: &common.StrategyInstanceInput{
			InstanceKey:      in.InstanceKey,
			TemplateId:       in.TemplateId,
			TemplateRevision: in.TemplateRevision,
			AccountId:        in.AccountId,
			InstrumentId:     in.InstrumentId,
			MarketDataSource: in.MarketDataSource,
			Parameters:       p,
		},
	})
}

// UpdateInstance changes the parameters of a stopped instance using version checks.
func (s *Server) UpdateInstance(ctx context.Context, in *pb.UpdateInstanceRequest) (*pb.Instance, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	p, err := parametersFromWire(in.GetParameters())
	if err != nil {
		return nil, err
	}
	return s.manage(ctx, in.Actor, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementUpdate,
		ID:                     in.Id,
		ExpectedRevision:       in.ExpectedRevision,
		ExpectedControlVersion: in.ExpectedControlVersion,
		Input:                  &common.StrategyInstanceInput{Parameters: p},
	})
}

// SetInstanceRunning changes the requested running state without hot reloading parameters.
func (s *Server) SetInstanceRunning(ctx context.Context, in *pb.RunningRequest) (*pb.Instance, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	operation := instanceconfig.ManagementStop
	if in.GetRunning() {
		row, err := s.row(ctx, in.Actor, in.Id)
		if err != nil {
			return nil, err
		}
		view, err := s.instance(ctx, row, s.inventories(ctx))
		if err != nil {
			return nil, err
		}
		if view.RestartRequired {
			return nil, status.Error(codes.FailedPrecondition, "restart core services to load this instance revision before starting")
		}
		operation = instanceconfig.ManagementStart
	}
	return s.manage(ctx, in.Actor, instanceconfig.ManagementWrite{
		Operation:              operation,
		ID:                     in.Id,
		ExpectedRevision:       in.ExpectedRevision,
		ExpectedControlVersion: in.ExpectedControlVersion,
	})
}
