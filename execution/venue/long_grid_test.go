package venue

import (
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLongGridRPCRejectsWrongRoleRevisionAndLegacyQuote(t *testing.T) {
	child := &Runtime{cfg: &venueconfig.RuntimeConfig{
		InstanceID: 2,
		Revision:   3,
		Template:   domain.StrategyTemplateGrid,
		LongGrid:   &common.LongGridParameters{},
	}}
	parent := &Instances{children: map[int64]*Runtime{2: child}}
	input := &pb.LongGridRequest{
		StrategyInstanceId: 2,
		StrategyRevision:   3,
	}
	if _, err := parent.EvaluateLongGrid(t.Context(), input); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong role = %v", err)
	}
	input.StrategyRevision = 2
	if _, err := parent.EvaluateLongGrid(washTestContext(t.Context()), input); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("wrong revision = %v", err)
	}
	if _, err := child.Reconcile(washTestContext(t.Context()), &pb.QuotePlan{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("legacy quote entered long-only runtime: %v", err)
	}
}
