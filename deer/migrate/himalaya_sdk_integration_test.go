//go:build integration

package migrate

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/gorm"

	himalayamigrate "github.com/hmlax/muskdeer/deer/migrate/himalaya"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestHimalayaSDKDurableAdmissionReservesBudgetAndRejectsStaleEpoch(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ClientID += "-second"
	if _, err := store.Prepare(ctx, lease, request); !errors.Is(err, himalaya.ErrRiskLimit) {
		t.Fatalf("shared account was over-reserved: %v", err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).
		Where("account_id = ?", lease.Key.AccountID).UpdateColumn("epoch", lease.Epoch+1).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, domain.ErrLeaseLost) || calls != 0 {
		t.Fatalf("old epoch escaped into SDK: calls=%d err=%v", calls, err)
	}
}

func TestHimalayaSDKClaimSurvivesReplyLossAndCannotBeDispatchedAgain(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("injected reply loss")
	calls := 0
	err = store.WithSubmission(ctx, prepared.Admission, func(context.Context) error {
		calls++
		// A separate connection must see the committed claim before the effect.
		var row executionmodelv1.HimalayaIntent
		if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil {
			return err
		}
		if row.State != himalaya.IntentDispatched {
			t.Errorf("intent was not durable before dispatch: %s", row.State)
		}
		return lost
	})
	if !errors.Is(err, lost) || calls != 1 {
		t.Fatalf("first dispatch = %d, %v", calls, err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); !errors.Is(err, domain.ErrOutcomeUnknown) || calls != 1 {
		t.Fatalf("ambiguous order was dispatched again: calls=%d err=%v", calls, err)
	}
	var row commonmodelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(row.ReservedNotional).Equal(decimal.NewFromInt(100)) {
		t.Fatal("reply loss released the budget reservation")
	}
}

func TestHimalayaSDKPersistedGuardStopsSecondActualSDKCall(t *testing.T) {
	_, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	peer := &sdkReplyLossPeer{}
	pb.RegisterOrderServer(server, peer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		<-done
	})
	client, err := himalaya.NewClient(ctx, himalaya.Config{
		Target:  "passthrough:///sdk-durable-test",
		Token:   "sdk-integration-test-token",
		Timeout: time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
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
	for range 2 {
		if _, err := client.Submit(ctx, prepared.Request); !errors.Is(err, domain.ErrOutcomeUnknown) {
			t.Fatalf("ambiguous SDK attempt lost its state: %v", err)
		}
	}
	if peer.calls.Load() != 1 {
		t.Fatalf("reply loss caused %d SDK calls", peer.calls.Load())
	}
}

type sdkReplyLossPeer struct {
	pb.UnimplementedOrderServer
	calls atomic.Int64
}

func (p *sdkReplyLossPeer) Place(context.Context, *pb.PlaceRequest) (*pb.PlaceOrderResponse, error) {
	p.calls.Add(1)
	return nil, status.Error(codes.Unavailable, "injected SDK reply loss")
}

func TestHimalayaSDKPersistedMutationAndExpiredRiskCannotDispatch(t *testing.T) {
	for _, change := range []string{"price", "snapshot_expires_at", "maximum_notional"} {
		t.Run(change, func(t *testing.T) {
			db, store, lease, request := sdkAdmissionFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			prepared, err := store.Prepare(ctx, lease, request)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "price":
				err = db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).UpdateColumn("price", "101").Error
			case "snapshot_expires_at":
				err = db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("snapshot_expires_at", 0).Error
			case "maximum_notional":
				err = db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("maximum_notional", "999").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatalf("changed intent or risk reached SDK: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestHimalayaSDKExtensionHistoryDoesNotChangeCoreLedger(t *testing.T) {
	db, _, _, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := himalayamigrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := CheckCurrent(ctx, db); err != nil {
		t.Fatalf("extension changed core migration history: %v", err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaSchemaMigration{}).Where("version = ?", 1).UpdateColumn("checksum", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := himalayamigrate.Apply(ctx, db); !errors.Is(err, himalayamigrate.ErrHistory) {
		t.Fatalf("changed SDK schema history was accepted: %v", err)
	}
}

func TestHimalayaSDKAcknowledgementRetainsTheWholeRiskReservation(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := store.Prepare(ctx, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	response := &pb.PlaceOrderResponse{
		OrderId:  42,
		ClientId: prepared.Request.ClientID,
	}
	for range 2 {
		if err := store.Acknowledge(ctx, prepared.Admission, response); err != nil {
			t.Fatal(err)
		}
	}
	var row executionmodelv1.HimalayaIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.HimalayaIntent{}).Where("id = ?", prepared.Request.IntentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != himalaya.IntentAcknowledged || row.OrderId != "42" {
		t.Fatal("known native identity was not persisted")
	}
	request.ClientID += "-next"
	if _, err := store.Prepare(ctx, lease, request); !errors.Is(err, himalaya.ErrRiskLimit) {
		t.Fatalf("acknowledgement released funds without terminal evidence: %v", err)
	}
}

func TestHimalayaSDKConcurrentPreparationsShareOneFundBudget(t *testing.T) {
	db, store, lease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index := range 2 {
		workers.Go(func() {
			<-start
			candidate := request
			candidate.ClientID += "-" + strconv.Itoa(index)
			_, err := store.Prepare(ctx, lease, candidate)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, himalaya.ErrRiskLimit):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	var row commonmodelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Where("account_id = ?", lease.Key.AccountID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if successes != 1 || rejected != 1 || !decimal.RequireFromString(row.ReservedNotional).Equal(decimal.NewFromInt(100)) {
		t.Fatalf("concurrent admissions overspent the fund: accepted=%d rejected=%d", successes, rejected)
	}
}

func sdkAdmissionFixture(t *testing.T) (*gorm.DB, *himalaya.Store, domain.LeaseToken, himalaya.SubmitRequest) {
	t.Helper()
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatalf("core migration fixture: %v; cause: %v", err, errors.Unwrap(err))
	}
	if err := himalayamigrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := himalaya.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "execution-sdk-test")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: himalaya.AccountID(10001, 10001),
	}, owner, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	budget := &commonmodelv1.HimalayaBudget{
		AccountId:         lease.Key.AccountID,
		UserId:            10001,
		FundId:            10001,
		MaximumNotional:   "1000",
		ObservedNotional:  "900",
		ReservedNotional:  "0",
		SnapshotExpiresAt: time.Now().Add(10 * time.Second).UnixMilli(),
		Enabled:           true,
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.HimalayaBudget{}).Create(budget).Error; err != nil {
		t.Fatal(err)
	}
	return db, store, lease, himalaya.SubmitRequest{
		OwnerID:  lease.OwnerID,
		Epoch:    uint64(lease.Epoch),
		UserID:   10001,
		FundID:   10001,
		Symbol:   "BTCUSDT",
		ClientID: "sdk-durable-test",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
		Leverage: 10,
	}
}
