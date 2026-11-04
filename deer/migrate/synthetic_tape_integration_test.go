//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestSyntheticTapeModelPreservesTypedAuditAndUniqueSlot(t *testing.T) {
	db, _, _, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	row := &executionmodelv1.SyntheticTapeIntent{
		RunId:        "test-tape-1",
		Symbol:       "BTCUSDT",
		Sequence:     0,
		PolicyDigest: "test-policy",
		Plan: &commonv1.SyntheticTapePlan{
			RunId:    "test-tape-1",
			Symbol:   "BTCUSDT",
			Quantity: "0.001",
			Price:    "100.1",
		},
		Notional: "0.1001",
		Result:   &commonv1.SyntheticTapeResult{State: native.SyntheticUnknown},
	}
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	var read executionmodelv1.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Where("id = ?", row.Id).First(&read).Error; err != nil {
		t.Fatal(err)
	}
	if read.Id <= 0 || read.CreateAt == 0 || read.UpdateAt == 0 || read.DeleteAt != nil || !proto.Equal(read.Plan, row.Plan) || !proto.Equal(read.Result, row.Result) {
		t.Fatal("synthetic audit lost typed fields or model timestamps")
	}
	duplicate := proto.CloneOf(row)
	duplicate.Id = 0
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Create(duplicate).Error; err == nil {
		t.Fatal("duplicate run/symbol/sequence was accepted")
	}
	if err := CheckCurrent(ctx, db); err != nil {
		t.Fatalf("synthetic extension changed the core ledger: %v", err)
	}
}
