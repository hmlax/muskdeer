//go:build integration

package console

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func verifyMakerStartBudget(ctx context.Context, t *testing.T, db *gorm.DB, actor *pb.Actor) {
	t.Helper()
	var row strategy.StrategyInstance
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", "status-filter-4").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var budget model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", row.AccountId).Take(&budget).Error; err != nil {
		t.Fatal(err)
	}
	actor = proto.CloneOf(actor)
	actor.Subject, actor.RequestId = "user:2", "maker-check-regression-start"
	server := &Server{
		db:       db,
		controls: true,
		accountInspector: func(_ context.Context, scope *common.VenueScope) (*common.VenueSnapshot, error) {
			return &common.VenueSnapshot{
				Scope:        proto.CloneOf(scope),
				ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
				Market: &common.VenueMarket{
					PriceTick:       "0.01",
					QuantityStep:    "1",
					MinimumQuantity: "1",
					MinimumNotional: "1",
					MaximumQuantity: "0",
					MaximumLeverage: 10,
				},
			}, nil
		},
		bookInspector: func(_ context.Context, symbol string) (*common.VenueBook, error) {
			return &common.VenueBook{
				Symbol:       symbol,
				Source:       budget.MarketDataSource,
				Segment:      "usdm",
				ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
				Bids: []*common.VenueLevel{
					{
						Price:    "10",
						Quantity: "1000",
					},
				},
				Asks: []*common.VenueLevel{
					{
						Price:    "11",
						Quantity: "1000",
					},
				},
			}, nil
		},
	}
	operator := operatorContext(ctx, rpcsecurity.ServiceOperator)
	check, err := server.CheckInstance(operator, &pb.InstanceRequest{
		Actor: actor,
		Id:    row.Id,
	})
	if err != nil || check.GetCanStart() || check.GetReason() != domain.MakerBudgetReason {
		t.Fatalf("missing common budget diagnostic: %v %v", check, err)
	}
	request := &pb.RunningRequest{
		Actor:                  actor,
		Id:                     row.Id,
		ExpectedRevision:       row.Revision,
		ExpectedControlVersion: row.RequestedConfigVersion,
		Running:                true,
	}
	if _, err := server.SetInstanceRunning(operator, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("underfunded console start accepted: %v", err)
	}
	var saved strategy.StrategyInstance
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&row, &saved) {
		t.Fatal("failed preflight changed control state")
	}
	foreign := proto.CloneOf(actor)
	foreign.AllowedAccountIds = []string{"foreign-account"}
	if _, err := server.CheckInstance(operator, &pb.InstanceRequest{
		Actor: foreign,
		Id:    row.Id,
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign diagnostic leaked: %v", err)
	}
	if _, err := server.CheckInstance(ctx, &pb.InstanceRequest{
		Actor: actor,
		Id:    row.Id,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("untrusted diagnostic accepted: %v", err)
	}
	verifyMakerStartTransaction(ctx, t, db, &row, &budget)
	server.accountInspector = func(context.Context, *common.VenueScope) (*common.VenueSnapshot, error) {
		t.Fatal("stop must not inspect the SDK")
		return nil, nil
	}
	request.Running = false
	if _, err := server.SetInstanceRunning(operator, request); err != nil {
		t.Fatalf("underfunded maker cannot stop: %v", err)
	}
}

func verifyMakerStartTransaction(ctx context.Context, t *testing.T, db *gorm.DB, row *strategy.StrategyInstance, budget *model.TradingAccountPolicy) {
	t.Helper()
	profile, err := venueconfig.ProfileFromBudget(budget)
	if err != nil {
		t.Fatal(err)
	}
	profile.StrategyInstanceId = row.Id
	var before int64
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		notional string
		expiry   int64
		updated  int64
		want     error
	}{
		{
			name:     "over budget",
			notional: "200",
			expiry:   time.Now().Add(time.Second).UnixMilli(),
			updated:  budget.UpdateAt,
			want:     instanceconfig.ErrMakerBudget,
		},
		{
			name:     "expired observation",
			notional: "50",
			updated:  budget.UpdateAt,
			want:     instanceconfig.ErrStartCheckExpired,
		},
		{
			name:     "concurrent policy change",
			notional: "50",
			expiry:   time.Now().Add(time.Second).UnixMilli(),
			updated:  budget.UpdateAt - 1,
			want:     instanceconfig.ErrConflict,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := instanceconfig.ManageConfigured(ctx, db, instanceconfig.ManagementWrite{
				Operation:              instanceconfig.ManagementStart,
				Subject:                "maker-check-test",
				RequestID:              "maker-check-transaction-regression",
				ID:                     row.Id,
				ExpectedRevision:       row.Revision,
				ExpectedControlVersion: row.RequestedConfigVersion,
				StartCheck: &instanceconfig.StartCheck{
					PolicyUpdatedAt:  test.updated,
					Profile:          profile,
					RequiredNotional: test.notional,
					ValidUntilMs:     test.expiry,
				},
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("start transaction accepted invalid proof: %v", err)
			}
		})
	}
	var after int64
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("rejected start appended history")
	}
	var saved strategy.StrategyInstance
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(row, &saved) {
		t.Fatal("rejected start mutated instance")
	}
}
