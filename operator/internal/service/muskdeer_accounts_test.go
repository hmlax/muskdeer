package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

type enrollmentConsole struct {
	core.ConsoleClient
	calls   int
	timeout bool
	last    *core.CreateAccountRequest
}

func (c *enrollmentConsole) CreateAccount(_ context.Context, request *core.CreateAccountRequest, _ ...grpc.CallOption) (*core.Account, error) {
	c.calls++
	c.last = proto.CloneOf(request)
	if c.timeout {
		return nil, status.Error(codes.DeadlineExceeded, "injected ambiguous reply")
	}
	user, _ := strconv.ParseInt(request.Data.UserId, 10, 64)
	fund, _ := strconv.ParseInt(request.Data.FundId, 10, 64)
	return &core.Account{
		Id:        99,
		AccountId: muskdeer.AccountScope(user, fund),
		UserId:    user,
		FundId:    fund,
		Symbol:    "BTCUSDT",
		Enabled:   true,
	}, nil
}

func TestMuskdeerAccountEnrollmentTimeoutRetainsPrivateOwnershipForRetry(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	_, err := db.Client().PermissionGroup.Create().SetName("market").SetModule(muskdeer.Module).Save(system)
	require.NoError(t, err)
	_, err = db.Client().Tenant.Create().SetID(7).SetName("merchant").SetCode("merchant").Save(system)
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = db.Client().MuskdeerAccountBinding.Create().SetTenantID(7).SetAccountID(muskdeer.AccountScope(100, 100)).SetIsEnabled(true).SetVersion(1).SetCreatedAt(now).SetUpdatedAt(now).Save(system)
	require.NoError(t, err)
	tenant := uint32(7)
	ctx := auth.NewContext(viewer.WithContext(t.Context(), viewer.NewUserContext(2, 7, 0, "", nil)), &pb.UserTokenPayload{
		UserId:   2,
		TenantId: &tenant,
	})
	client := &enrollmentConsole{timeout: true}
	bindings := data.NewMuskdeerBindingRepo(db)
	service := &MuskdeerService{
		client:   client,
		bindings: bindings,
		log:      bLogger.NewHelper(bLogger.NopLogger()),
	}
	request := &pb.MuskdeerCreateAccountRequest{Data: &pb.MuskdeerAccountDraft{
		UserId:               "100",
		FundId:               "101",
		InstrumentId:         "1",
		MarketDataSource:     "binance",
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		Leverage:             10,
	}}
	_, err = service.CreateAccount(ctx, request)
	require.Error(t, err)
	scope, err := bindings.Scope(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{muskdeer.AccountScope(100, 100)}, scope, "an unconfirmed fund cannot expose business data")
	client.timeout = false
	result, err := service.CreateAccount(ctx, request)
	require.NoError(t, err)
	require.Equal(t, muskdeer.AccountScope(100, 101), result.AccountId)
	require.Equal(t, uint64(7), client.last.Actor.TenantId)
	require.ElementsMatch(t, []string{muskdeer.AccountScope(100, 100), result.AccountId}, client.last.Actor.AllowedAccountIds)
	scope, err = bindings.Scope(ctx)
	require.NoError(t, err)
	require.Len(t, scope, 2)
	_, err = service.CreateAccount(ctx, request)
	require.Error(t, err, "adding an active account must not overwrite its current policy")
	require.Equal(t, 2, client.calls, "duplicate add reached core mutation")
}
