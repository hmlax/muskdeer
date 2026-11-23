package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const muskdeerErrorReason = "MUSKDEER_MANAGEMENT_ERROR"
const muskdeerMerchantRequired = "merchant context required for trading operations"

type MuskdeerService struct {
	pb.UnimplementedMuskdeerServiceServer
	client   core.ConsoleClient
	bindings *data.MuskdeerBindingRepo
	log      *bLogger.Helper
}

func NewMuskdeerService(ctx *bootstrap.Context, connection *data.MuskdeerConnection, bindings *data.MuskdeerBindingRepo, permissions *PermissionService) (*MuskdeerService, error) {
	if runtimeconfig.SettingsFromContext(ctx).Muskdeer.Enabled {
		seed, cancel := context.WithTimeout(viewer.WithSystemContext(ctx.Context()), 30*time.Second)
		defer cancel()
		if err := bindings.CheckSchema(seed); err != nil {
			return nil, err
		}
		if err := permissions.apiRepo.EnsureMuskdeerModule(seed); err != nil {
			return nil, err
		}
		if err := permissions.apiRepo.EnsureBuiltInAdminAPIs(seed); err != nil {
			return nil, err
		}
		if err := permissions.authorizer.ResetPolicies(seed); err != nil {
			return nil, err
		}
	}
	return &MuskdeerService{
		client:   connection.Client(),
		bindings: bindings,
		log:      ctx.NewLoggerHelper("muskdeer/service/operator"),
	}, nil
}
func (s *MuskdeerService) actor(ctx context.Context, scope bool) (*core.Actor, error) {
	identity, err := data.MuskdeerIdentity(ctx, false)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	if scope && identity.GetTenantId() == 0 {
		return nil, s.failure(ctx, status.Error(codes.PermissionDenied, muskdeerMerchantRequired))
	}
	actor := &core.Actor{
		Subject:   fmt.Sprintf("user:%d", identity.UserId),
		TenantId:  uint64(identity.GetTenantId()),
		RequestId: rand.Text(),
	}
	if scope {
		actor.AllowedAccountIds, err = s.bindings.Scope(ctx)
		if err != nil {
			return nil, s.failure(ctx, err)
		}
	}
	return actor, nil
}
func (s *MuskdeerService) failure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	s.log.Errorf(ctx, "Muskdeer request failed: %v", err)
	code := 500
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = 400
	case codes.PermissionDenied:
		code = 403
	case codes.Unauthenticated:
		code = 401
	case codes.NotFound:
		code = 404
	case codes.Aborted, codes.AlreadyExists, codes.FailedPrecondition:
		code = 409
	case codes.Unavailable:
		code = 503
	case codes.DeadlineExceeded:
		code = 504
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		code = 404
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		code = 409
	}
	// Keep SQL, network addresses and internal error details in operator logs.
	message := "Muskdeer request failed"
	if code == 409 {
		message = "refresh the current version; instance must be stopped for changes and loaded by core services before starting"
	}
	if code == 403 {
		message = "permission or trading account binding required"
	}
	if code == 503 {
		message = "Muskdeer service unavailable"
	}
	if code == 504 {
		message = "request timed out; refresh before retrying"
	}
	return kratoserrors.New(code, muskdeerErrorReason, message)
}
func (s *MuskdeerService) GetOverview(ctx context.Context, in *pb.MuskdeerOverviewRequest) (*pb.MuskdeerOverview, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.GetOverview(ctx, &core.ActorRequest{
		Actor: actor,
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreOverview(result), nil
}
func (s *MuskdeerService) ListTemplates(ctx context.Context, in *pb.MuskdeerListRequest) (*pb.MuskdeerTemplatePage, error) {
	actor, err := s.actor(ctx, false)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListTemplates(ctx, &core.ListRequest{
		Actor:    actor,
		Page:     in.GetPage(),
		PageSize: in.GetPageSize(),
		Search:   in.GetSearch(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreTemplatePage(result), nil
}
func (s *MuskdeerService) ListGateways(ctx context.Context, in *pb.MuskdeerOverviewRequest) (*pb.MuskdeerGatewayPage, error) {
	if _, err := data.MuskdeerIdentity(ctx, true); err != nil {
		return nil, s.failure(ctx, err)
	}
	actor, err := s.actor(ctx, false)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListGateways(ctx, &core.ActorRequest{Actor: actor})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreGatewayPage(result), nil
}
func (s *MuskdeerService) ListInstruments(ctx context.Context, in *pb.MuskdeerListRequest) (*pb.MuskdeerInstrumentPage, error) {
	identity, err := data.MuskdeerIdentity(ctx, false)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	// Platform owns the catalog; merchant forms select within their bound accounts.
	actor, err := s.actor(ctx, identity.GetTenantId() != 0)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListInstruments(ctx, &core.ListRequest{
		Actor:    actor,
		Page:     in.GetPage(),
		PageSize: in.GetPageSize(),
		Search:   in.GetSearch(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstrumentPage(result), nil
}
func (s *MuskdeerService) ListAccounts(ctx context.Context, in *pb.MuskdeerListRequest) (*pb.MuskdeerAccountPage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListAccounts(ctx, &core.ListRequest{
		Actor:     actor,
		Page:      in.GetPage(),
		PageSize:  in.GetPageSize(),
		Search:    in.GetSearch(),
		AccountId: in.GetAccountId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreAccountPage(result), nil
}
func (s *MuskdeerService) ListInstances(ctx context.Context, in *pb.MuskdeerInstanceListRequest) (*pb.MuskdeerInstancePage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListInstances(ctx, &core.InstanceListRequest{
		Actor:          actor,
		Page:           in.GetPage(),
		PageSize:       in.GetPageSize(),
		Search:         in.GetSearch(),
		AccountId:      in.GetAccountId(),
		StrategyType:   in.GetStrategyType(),
		DesiredRunning: in.DesiredRunning,
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstancePage(result), nil
}
func (s *MuskdeerService) GetInstance(ctx context.Context, in *pb.MuskdeerInstanceRequest) (*pb.MuskdeerInstance, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.GetInstance(ctx, &core.InstanceRequest{
		Actor: actor,
		Id:    in.GetId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstance(result), nil
}
func (s *MuskdeerService) GetAccountSnapshot(ctx context.Context, in *pb.MuskdeerAccountRequest) (*pb.MuskdeerAccountSnapshot, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.GetAccountSnapshot(ctx, &core.AccountRequest{
		Actor:     actor,
		AccountId: in.GetAccountId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreAccountSnapshot(result), nil
}
func (s *MuskdeerService) ListOrderRecords(ctx context.Context, in *pb.MuskdeerRecordListRequest) (*pb.MuskdeerOrderPage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListOrderRecords(ctx, &core.RecordListRequest{
		Actor:      actor,
		Page:       in.GetPage(),
		PageSize:   in.GetPageSize(),
		Symbol:     in.GetSymbol(),
		AccountId:  in.GetAccountId(),
		State:      in.GetState(),
		InstanceId: in.GetInstanceId(),
		RunId:      in.GetRunId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreOrderPage(result), nil
}
func (s *MuskdeerService) ListCancellations(ctx context.Context, in *pb.MuskdeerRecordListRequest) (*pb.MuskdeerCancellationPage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListCancellations(ctx, &core.RecordListRequest{
		Actor:      actor,
		Page:       in.GetPage(),
		PageSize:   in.GetPageSize(),
		Symbol:     in.GetSymbol(),
		AccountId:  in.GetAccountId(),
		State:      in.GetState(),
		InstanceId: in.GetInstanceId(),
		RunId:      in.GetRunId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreCancellationPage(result), nil
}
func (s *MuskdeerService) ListSyntheticRecords(ctx context.Context, in *pb.MuskdeerRecordListRequest) (*pb.MuskdeerSyntheticPage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.ListSyntheticRecords(ctx, &core.RecordListRequest{
		Actor:      actor,
		Page:       in.GetPage(),
		PageSize:   in.GetPageSize(),
		Symbol:     in.GetSymbol(),
		AccountId:  in.GetAccountId(),
		State:      in.GetState(),
		InstanceId: in.GetInstanceId(),
		RunId:      in.GetRunId(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreSyntheticPage(result), nil
}
func (s *MuskdeerService) CancelInstanceOrders(ctx context.Context, in *pb.MuskdeerCancelRequest) (*pb.MuskdeerCancelReply, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.CancelInstanceOrders(ctx, &core.CancelRequest{
		Actor:            actor,
		InstanceId:       in.GetInstanceId(),
		ExpectedRevision: in.GetExpectedRevision(),
		CancelAll:        in.GetCancelAll(),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreCancelReply(result), nil
}
func (s *MuskdeerService) CreateInstance(ctx context.Context, in *pb.MuskdeerCreateInstanceRequest) (*pb.MuskdeerInstance, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	if in.GetData() == nil {
		return nil, kratoserrors.BadRequest(muskdeerErrorReason, "instance data required")
	}
	v := in.Data
	result, err := s.client.CreateInstance(ctx, &core.CreateInstanceRequest{
		Actor:            actor,
		InstanceKey:      v.InstanceKey,
		TemplateId:       v.TemplateId,
		TemplateRevision: v.TemplateRevision,
		AccountId:        v.AccountId,
		InstrumentId:     v.InstrumentId,
		MarketDataSource: v.MarketDataSource,
		Parameters:       toCoreInstanceParameters(v.Parameters),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstance(result), nil
}
func (s *MuskdeerService) UpdateInstance(ctx context.Context, in *pb.MuskdeerUpdateInstanceRequest) (*pb.MuskdeerInstance, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	if in.GetData() == nil {
		return nil, kratoserrors.BadRequest(muskdeerErrorReason, "instance data required")
	}
	result, err := s.client.UpdateInstance(ctx, &core.UpdateInstanceRequest{
		Actor:                  actor,
		Id:                     in.Id,
		ExpectedRevision:       in.ExpectedRevision,
		ExpectedControlVersion: in.ExpectedControlVersion,
		Parameters:             toCoreInstanceParameters(in.Data.Parameters),
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstance(result), nil
}
func (s *MuskdeerService) StartInstance(ctx context.Context, in *pb.MuskdeerRunningRequest) (*pb.MuskdeerInstance, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.SetInstanceRunning(ctx, &core.RunningRequest{
		Actor:                  actor,
		Id:                     in.GetId(),
		ExpectedRevision:       in.GetExpectedRevision(),
		ExpectedControlVersion: in.GetExpectedControlVersion(),
		Running:                true,
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstance(result), nil
}
func (s *MuskdeerService) StopInstance(ctx context.Context, in *pb.MuskdeerRunningRequest) (*pb.MuskdeerInstance, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	result, err := s.client.SetInstanceRunning(ctx, &core.RunningRequest{
		Actor:                  actor,
		Id:                     in.GetId(),
		ExpectedRevision:       in.GetExpectedRevision(),
		ExpectedControlVersion: in.GetExpectedControlVersion(),
		Running:                false,
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return fromCoreInstance(result), nil
}
func (s *MuskdeerService) ListAccountBindings(ctx context.Context, in *pb.MuskdeerBindingListRequest) (*pb.MuskdeerBindingPage, error) {
	result, err := s.bindings.List(ctx, in)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return result, nil
}
func (s *MuskdeerService) CreateAccountBinding(ctx context.Context, in *pb.MuskdeerCreateBindingRequest) (*pb.MuskdeerAccountBinding, error) {
	if _, err := data.MuskdeerIdentity(ctx, true); err != nil {
		return nil, s.failure(ctx, err)
	}
	if in.GetData() == nil {
		return nil, kratoserrors.BadRequest(muskdeerErrorReason, "binding data required")
	}
	actor, err := s.actor(ctx, false)
	if err != nil {
		return nil, err
	}
	// Exact account lookup through gRPC, never the business database.
	accounts, err := s.client.ListAccounts(ctx, &core.ListRequest{
		Actor:     actor,
		AccountId: in.Data.AccountId,
		Page:      1,
		PageSize:  1,
	})
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	if accounts.GetTotal() != 1 {
		return nil, kratoserrors.BadRequest(muskdeerErrorReason, "trading account not found")
	}
	result, err := s.bindings.Create(ctx, in.Data)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return result, nil
}
func (s *MuskdeerService) SetAccountBindingEnabled(ctx context.Context, in *pb.MuskdeerSetBindingEnabledRequest) (*pb.MuskdeerAccountBinding, error) {
	result, err := s.bindings.SetEnabled(ctx, in)
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return result, nil
}
