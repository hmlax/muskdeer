//go:build integration

package migrate

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestHimalayaFailedStreamCreationCannotLeaveUnknownOrder(t *testing.T) {
	db, store, lease, order := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, order)
	if err != nil {
		t.Fatal(err)
	}
	client, err := himalaya.NewClient(ctx, himalaya.Config{
		Target:  "passthrough:///unreachable-sdk-fixture",
		Token:   "test-token",
		Timeout: time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return nil, errors.New("injected failure before request stream")
			}),
		},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if _, err := client.Submit(ctx, prepared.Request); err == nil || errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("Submit(no request stream) = %v, want known no-effect failure", err)
	}
	var row modelv1.HimalayaIntent
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	budget, err := store.Budget(ctx, lease.Key.AccountID)
	if err != nil || row.State != himalaya.IntentTerminal || !decimal.RequireFromString(budget.GetReservedNotional()).IsZero() {
		t.Fatalf("unsent stream failure state=%s reserved=%s err=%v, want terminal/zero", row.State, budget.GetReservedNotional(), err)
	}
}
