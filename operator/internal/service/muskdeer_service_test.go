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
	require.Equal(t, "9007199254740993.123456789012345678", accounts.Items[0].MaximumNotional)
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
	for _, request := range []proto.Message{&pb.MuskdeerListRequest{}, &pb.MuskdeerCreateInstanceRequest{}, &pb.MuskdeerRunningRequest{}} {
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
