//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestSyntheticZeroLimitsContinuePastOneThousandWithoutReplayingUnknowns(t *testing.T) {
	db, store, lease, cfg, base := syntheticDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cfg.SyntheticTape.MaximumEvents = 0
	cfg.SyntheticTape.Profiles[0].MaximumNotional = "0"
	digest, err := native.SyntheticPolicyDigest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]*model.SyntheticTapeIntent, 0, 1001)
	for sequence := range 1001 {
		plan, err := native.SyntheticPlan(cfg, base.Symbol, uint64(sequence), base.Book, base.Strategy)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, &model.SyntheticTapeIntent{
			CreateAt:     time.Now().Add(-time.Minute).UnixMilli(),
			UpdateAt:     time.Now().Add(-time.Minute).UnixMilli(),
			RunId:        plan.RunId,
			Symbol:       plan.Symbol,
			Sequence:     int64(sequence),
			PolicyDigest: digest,
			Plan:         plan,
			Notional:     decimal.RequireFromString(plan.Price).Mul(decimal.RequireFromString(plan.Quantity)).String(),
			Result:       &common.SyntheticTapeResult{State: native.SyntheticUnknown},
		})
	}
	if err := db.WithContext(ctx).Model(&model.SyntheticTapeIntent{}).CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	// Capture the actual stored decimal representation and generated timestamps.
	// Compare every persisted field across publication, rather than comparing
	// PostgreSQL numeric formatting with the pre-insert fixture strings.
	var before []*model.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&model.SyntheticTapeIntent{}).Order("sequence").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	book := proto.CloneOf(base.Book)
	book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	plan, err := native.SyntheticPlan(cfg, base.Symbol, 1001, book, base.Strategy)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	send := func(_ context.Context, input *gateway.SyntheticRequest) (*gateway.SyntheticResult, error) {
		calls++
		return &gateway.SyntheticResult{
			TradeID:     "continued-development-slot",
			Price:       input.Price,
			Quantity:    input.Quantity,
			TimestampMS: uint64(time.Now().UnixMilli()),
		}, nil
	}
	result, err := store.PublishSynthetic(ctx, lease, cfg, plan, send)
	if err != nil || result.GetState() != native.SyntheticPublished || calls != 1 {
		t.Fatalf("PublishSynthetic(sequence 1001, zero limits) = %v, %v, calls=%d, want one new publication", result, err, calls)
	}
	book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	replay, err := native.SyntheticPlan(cfg, base.Symbol, 1000, book, base.Strategy)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := store.PublishSynthetic(ctx, lease, cfg, replay, send); err != nil || !result.GetReplayed() || result.GetState() != native.SyntheticUnknown || calls != 1 {
		t.Fatalf("PublishSynthetic(old unknown) = %v, %v, calls=%d, want cached unknown without resend", result, err, calls)
	}
	var persisted []*model.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&model.SyntheticTapeIntent{}).Order("sequence").Find(&persisted).Error; err != nil || len(persisted) != 1002 {
		t.Fatalf("durable slots = %d, err=%v, want 1002", len(persisted), err)
	}
	for i := range before {
		if !proto.Equal(before[i], persisted[i]) {
			t.Fatalf("continued run changed old unknown slot %d: before=%s after=%s", i, protojson.Format(before[i]), protojson.Format(persisted[i]))
		}
	}
	if err := db.WithContext(ctx).Model(&model.SyntheticTapeIntent{}).Where("id = ?", rows[0].Id).UpdateColumn("notional", "999").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishSynthetic(ctx, lease, cfg, replay, send); err == nil || calls != 1 {
		t.Fatalf("PublishSynthetic(corrupt first history page) err=%v calls=%d, want rejection without effect", err, calls)
	}
}
