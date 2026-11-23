//go:build integration

package console

import (
	"context"
	"testing"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func verifyAccountParameterScope(ctx context.Context, t *testing.T, db *gorm.DB, actor *pb.Actor) {
	t.Helper()
	var policy model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", actor.AllowedAccountIds[0]).Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := venueconfig.ProfileFromBudget(&policy)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := &Server{
		db:       db,
		controls: true,
		accountPolicyUpdater: func(_ context.Context, in *execution.AccountPolicyUpdate) (*execution.AccountPolicyResult, error) {
			calls++
			if in.GetScope().GetUserId() != policy.UserId || in.GetScope().GetFundId() != policy.FundId || in.ExpectedUpdatedAtMs != policy.UpdateAt {
				t.Fatal("account scope or version lost")
			}
			return &execution.AccountPolicyResult{
				Id:          policy.Id,
				UpdatedAtMs: policy.UpdateAt,
				Enabled:     policy.Enabled,
				Profile:     profile,
			}, nil
		},
	}
	request := &pb.UpdateAccountRequest{
		Actor: &pb.Actor{
			Subject:           "user:2",
			TenantId:          7,
			RequestId:         "account-policy-console-test",
			AllowedAccountIds: actor.AllowedAccountIds,
		},
		Id:                  policy.Id,
		ExpectedUpdatedAtMs: policy.UpdateAt,
		Parameters: &pb.AccountParameters{
			MaximumNotional:      profile.MaximumNotional,
			MaximumOrderNotional: profile.MaximumOrderNotional,
			Leverage:             profile.Leverage,
		},
	}
	trusted := operatorContext(ctx, rpcsecurity.ServiceOperator)
	if _, err := server.UpdateAccount(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("untrusted update accepted")
	}
	request.Actor.AllowedAccountIds = []string{"foreign"}
	if _, err := server.UpdateAccount(trusted, request); status.Code(err) != codes.NotFound {
		t.Fatal("foreign update reached execution")
	}
	request.Actor.AllowedAccountIds = actor.AllowedAccountIds
	request.ExpectedUpdatedAtMs--
	if _, err := server.UpdateAccount(trusted, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("stale update reached execution")
	}
	if calls != 0 {
		t.Fatal("rejected request reached the financial writer")
	}
	request.ExpectedUpdatedAtMs = policy.UpdateAt
	if _, err := server.UpdateAccount(trusted, request); err != nil || calls != 1 {
		t.Fatalf("scoped update failed: %v", err)
	}
}
