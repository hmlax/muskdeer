package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

type capturingConsole struct {
	core.ConsoleClient
	actor   *core.Actor
	running bool
	calls   int
	filter  *core.InstanceListRequest
	disable *core.DisableAccountRequest
	enable  *core.EnableAccountRequest
}

func (c *capturingConsole) UpdateAccount(_ context.Context, in *core.UpdateAccountRequest, _ ...grpc.CallOption) (*core.Account, error) {
	c.actor = in.Actor
	c.calls++
	return &core.Account{
		Id:                   in.Id,
		MaximumNotional:      in.GetParameters().GetMaximumNotional(),
		MaximumOrderNotional: in.GetParameters().GetMaximumOrderNotional(),
		Leverage:             in.GetParameters().GetLeverage(),
	}, nil
}

func (c *capturingConsole) CheckInstance(_ context.Context, in *core.InstanceRequest, _ ...grpc.CallOption) (*core.MakerCheck, error) {
	c.actor = in.Actor
	c.calls++
	return &core.MakerCheck{
		TargetNotional:  "3998.123456789012345678",
		MaximumNotional: "100",
	}, nil
}

func (c *capturingConsole) EnableAccount(_ context.Context, in *core.EnableAccountRequest, _ ...grpc.CallOption) (*core.Account, error) {
	c.enable = proto.CloneOf(in)
	c.calls++
	return &core.Account{
		Id:          in.Id,
		Enabled:     true,
		UpdatedAtMs: in.ExpectedUpdatedAtMs,
	}, nil
}

func (c *capturingConsole) ListInstances(_ context.Context, in *core.InstanceListRequest, _ ...grpc.CallOption) (*core.InstancePage, error) {
	c.filter = proto.CloneOf(in)
	c.actor = in.Actor
	c.calls++
	return &core.InstancePage{}, nil
}

func (c *capturingConsole) ListAccounts(_ context.Context, in *core.ListRequest, _ ...grpc.CallOption) (*core.AccountPage, error) {
	c.actor = in.Actor
	c.calls++
	return &core.AccountPage{Items: []*core.Account{{
		AccountId:       "venue/7/7",
		MaximumNotional: "9007199254740993.123456789012345678",
	}}}, nil
}
func (c *capturingConsole) SetInstanceRunning(_ context.Context, in *core.RunningRequest, _ ...grpc.CallOption) (*core.Instance, error) {
	c.actor = in.Actor
	c.running = in.Running
	c.calls++
	return &core.Instance{
		Id:             in.Id,
		DesiredRunning: in.Running,
	}, nil
}

func TestMuskdeerFacadeDerivesScopeAndFixesStartStopIntent(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	_, err := db.Client().MuskdeerAccountBinding.Create().SetTenantID(7).SetAccountID("venue/7/7").SetIsEnabled(true).SetVersion(1).SetCreatedAt(time.Now().UTC()).SetUpdatedAt(time.Now().UTC()).Save(system)
	require.NoError(t, err)
	client := &capturingConsole{}
	service := &MuskdeerService{
		client:   client,
		bindings: data.NewMuskdeerBindingRepo(db),
		log:      bLogger.NewHelper(bLogger.NopLogger()),
	}
	tenantID := uint32(7)
	ctx := auth.NewContext(viewer.WithContext(t.Context(), viewer.NewUserContext(42, 7, 0, "", nil)), &pb.UserTokenPayload{
		UserId:   42,
		TenantId: &tenantID,
	})
	accounts, err := service.ListAccounts(ctx, &pb.MuskdeerListRequest{AccountId: "foreign/account"})
	require.NoError(t, err)
	require.Equal(t, []string{"venue/7/7"}, client.actor.AllowedAccountIds)
	require.Equal(t, uint64(7), client.actor.TenantId)
	require.Equal(t, "user:42", client.actor.Subject)
	require.NotEmpty(t, client.actor.RequestId)
	for _, filter := range []*bool{nil, proto.Bool(false), proto.Bool(true)} {
		_, err := service.ListInstances(ctx, &pb.MuskdeerInstanceListRequest{DesiredRunning: filter})
		require.NoError(t, err)
		require.Equal(t, filter, client.filter.DesiredRunning)
		require.Equal(t, []string{"venue/7/7"}, client.filter.Actor.AllowedAccountIds)
	}
	require.Equal(t, "9007199254740993.123456789012345678", accounts.Items[0].MaximumNotional)
	check, err := service.CheckInstance(ctx, &pb.MuskdeerInstanceRequest{Id: 213})
	require.NoError(t, err)
	require.Equal(t, "3998.123456789012345678", check.TargetNotional)
	require.Equal(t, []string{"venue/7/7"}, client.actor.AllowedAccountIds)

	updated, err := service.UpdateAccount(ctx, &pb.MuskdeerUpdateAccountRequest{
		Id:                  17,
		ExpectedUpdatedAtMs: 12345,
		Parameters: &pb.MuskdeerAccountParameters{
			MaximumNotional:      "9007199254740993.123456789012345678",
			MaximumOrderNotional: "100.000000000000000001",
			Leverage:             5,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "9007199254740993.123456789012345678", updated.MaximumNotional)
	require.Equal(t, "100.000000000000000001", updated.MaximumOrderNotional)
	require.Equal(t, []string{"venue/7/7"}, client.actor.AllowedAccountIds)
	enabled, err := service.EnableAccount(ctx, &pb.MuskdeerEnableAccountRequest{
		Id:                  17,
		ExpectedUpdatedAtMs: 12345,
	})
	require.NoError(t, err)
	require.True(t, enabled.Enabled)
	require.Equal(t, int64(17), client.enable.Id)
	require.Equal(t, int64(12345), client.enable.ExpectedUpdatedAtMs)
	require.Equal(t, []string{"venue/7/7"}, client.enable.Actor.AllowedAccountIds)
	disabled, err := service.DisableAccount(ctx, &pb.MuskdeerDisableAccountRequest{
		Id:                  17,
		ExpectedUpdatedAtMs: 12345,
	})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Equal(t, int64(12345), client.disable.ExpectedUpdatedAtMs)
	require.Equal(t, []string{"venue/7/7"}, client.disable.Actor.AllowedAccountIds)
	_, err = service.StartInstance(ctx, &pb.MuskdeerRunningRequest{Id: 12})
	require.NoError(t, err)
	require.True(t, client.running)
	_, err = service.StopInstance(ctx, &pb.MuskdeerRunningRequest{Id: 12})
	require.NoError(t, err)
	require.False(t, client.running)
	calls := client.calls
	_, err = service.CreateAccountBinding(ctx, &pb.MuskdeerCreateBindingRequest{Data: &pb.MuskdeerBindingDraft{
		TenantId:  9,
		AccountId: "foreign",
	}})
	require.Error(t, err)
	require.Equal(t, calls, client.calls, "merchant cannot reach core platform binding verification")
	_, err = service.ListAccounts(t.Context(), &pb.MuskdeerListRequest{})
	require.Error(t, err)
	require.Equal(t, calls, client.calls)
	// Identity/scope fields cannot be supplied in browser or gateway request DTOs.
	for _, request := range []proto.Message{&pb.MuskdeerListRequest{}, &pb.MuskdeerCreateInstanceRequest{}, &pb.MuskdeerRunningRequest{}, &pb.MuskdeerEnableAccountRequest{}, &pb.MuskdeerDisableAccountRequest{}} {
		fields := request.ProtoReflect().Descriptor().Fields()
		require.Nil(t, fields.ByName("actor"))
		require.Nil(t, fields.ByName("allowed_account_ids"))
	}
}

func TestLongGridParametersSurviveCoreAndOperatorMapping(t *testing.T) {
	input := &core.InstanceParameters{
		Execution: &core.ExecutionParameters{Leverage: 10},
		Algorithm: &core.InstanceParameters_LongGrid{LongGrid: &core.LongGridParameters{
			SpacingFraction:         "0.01",
			StopLossFraction:        "0.005",
			OrderNotional:           "100",
			MaximumPositionNotional: "100",
			OpenOnStart:             true,
			HaltAfterStopLoss:       true,
		}},
	}
	mapped := fromCoreInstanceParameters(input)
	require.NotNil(t, mapped.GetLongGrid())
	require.Equal(t, "0.005", mapped.GetLongGrid().StopLossFraction)
	require.True(t, proto.Equal(input, toCoreInstanceParameters(mapped)))
}

func (c *capturingConsole) DisableAccount(_ context.Context, in *core.DisableAccountRequest, _ ...grpc.CallOption) (*core.Account, error) {
	c.actor = in.Actor
	c.calls++
	c.disable = proto.CloneOf(in)
	return &core.Account{
		Id:          in.Id,
		Enabled:     false,
		UpdatedAtMs: in.ExpectedUpdatedAtMs + 1,
	}, nil
}
