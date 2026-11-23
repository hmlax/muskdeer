//go:build integration

package console

import (
	"context"
	"errors"
	"sync"
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
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

func verifyDisabledAccountStart(ctx context.Context, t *testing.T, db *gorm.DB, server *Server, actor *pb.Actor) (*strategy.StrategyInstance, *model.TradingAccountPolicy, int64) {
	t.Helper()
	var row strategy.StrategyInstance
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", "status-filter-3").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var policy model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", row.AccountId).Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", policy.Id).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	var historyBefore int64
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&historyBefore).Error; err != nil {
		t.Fatal(err)
	}
	command := instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                "status-filter-test",
		RequestID:              "disabled-account-start-regression",
		ID:                     row.Id,
		ExpectedRevision:       row.Revision,
		ExpectedControlVersion: row.RequestedConfigVersion,
	}
	if _, err := instanceconfig.ManageConfigured(ctx, db, command); !errors.Is(err, instanceconfig.ErrAccountDisabled) {
		t.Fatalf("disabled start accepted: %v", err)
	}
	var saved strategy.StrategyInstance
	var historyAfter int64
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&historyAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&row, &saved) || historyAfter != historyBefore {
		t.Fatal("rejected start mutated state or history")
	}
	server.controls = true
	actor.Subject, actor.RequestId = "user:2", command.RequestID
	operator := operatorContext(ctx, rpcsecurity.ServiceOperator)
	if _, err := server.manage(operator, actor, command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("missing public precondition: %v", err)
	}
	command.Operation = instanceconfig.ManagementStop
	if _, err := instanceconfig.ManageConfigured(ctx, db, command); err != nil {
		t.Fatalf("disabled account must still allow stop: %v", err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", policy.Id).Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	return &row, &policy, historyBefore
}

func verifyAccountEnable(ctx context.Context, t *testing.T, db *gorm.DB, server *Server, actor *pb.Actor, product *model.Instrument, accounts []string, row *strategy.StrategyInstance, policy *model.TradingAccountPolicy, historyBefore int64) {
	t.Helper()
	operator := operatorContext(ctx, rpcsecurity.ServiceOperator)
	var saved strategy.StrategyInstance
	var historyAfter int64
	command := instanceconfig.ManagementWrite{
		Operation:        instanceconfig.ManagementStart,
		Subject:          "status-filter-test",
		RequestID:        "enabled-account-start-regression",
		ID:               row.Id,
		ExpectedRevision: row.Revision,
	}
	request := &pb.EnableAccountRequest{
		Actor:               actor,
		Id:                  policy.Id,
		ExpectedUpdatedAtMs: policy.UpdateAt,
	}
	request.ExpectedUpdatedAtMs--
	if _, err := server.EnableAccount(operator, request); status.Code(err) != codes.Aborted {
		t.Fatalf("stale enable accepted: %v", err)
	}
	request.ExpectedUpdatedAtMs = policy.UpdateAt
	foreign := proto.CloneOf(request)
	foreign.Actor.AllowedAccountIds = accounts[1:2]
	if _, err := server.EnableAccount(operator, foreign); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign enable accepted: %v", err)
	}
	wrongFund := true
	server.accountInspector = func(_ context.Context, scope *common.VenueScope) (*common.VenueSnapshot, error) {
		observed := proto.CloneOf(scope)
		if wrongFund {
			observed.FundId++
		}
		return &common.VenueSnapshot{
			Scope:        observed,
			ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
			Market: &common.VenueMarket{
				PriceTick:       product.PriceTick,
				QuantityStep:    product.QuantityStep,
				MaximumLeverage: policy.Leverage,
			},
		}, nil
	}
	if _, err := server.EnableAccount(operator, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign SDK snapshot accepted: %v", err)
	}
	wrongFund = false
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() {
			result, err := server.EnableAccount(operator, request)
			if err == nil && !result.GetEnabled() {
				err = errors.New("account was not enabled")
			}
			results <- err
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent enable failed: %v", err)
		}
	}
	var enabled model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", policy.Id).Take(&enabled).Error; err != nil {
		t.Fatal(err)
	}
	policy.Enabled, policy.UpdateAt = true, enabled.UpdateAt
	if !proto.Equal(policy, &enabled) {
		t.Fatal("enable changed account parameters")
	}
	if _, err := server.EnableAccount(operator, request); err != nil {
		t.Fatalf("enable replay failed: %v", err)
	}
	var replay model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", policy.Id).Take(&replay).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&enabled, &replay) {
		t.Fatal("enable replay changed policy")
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&historyAfter).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DesiredRunning || saved.RequestedConfigVersion != row.RequestedConfigVersion+1 || historyAfter != historyBefore+1 {
		t.Fatal("account activation changed instance control or history")
	}
	command.Operation = instanceconfig.ManagementStart
	command.ExpectedControlVersion = saved.RequestedConfigVersion
	if _, err := instanceconfig.ManageConfigured(ctx, db, command); err != nil {
		t.Fatalf("enabled account cannot start: %v", err)
	}
}
