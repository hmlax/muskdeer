package repository

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminFlatteningProgressPreservesExactIdentityAndRejectsInventedCompletion(t *testing.T) {
	t.Parallel()
	row := &modelv1.AdminAccountFlatteningTask{
		Id:                 9007199254740993,
		CreateAt:           1000,
		UpdateAt:           1000,
		FlatteningId:       9007199254740995,
		StrategyInstanceId: 9007199254740997,
		AccountId:          "flatten-progress-account",
		InstrumentId:       9007199254740999,
		State:              string(domain.AdminFlattenQueued),
		Revision:           1,
	}
	queued, err := adminFlattenProgress(row)
	if err != nil || queued.ID != row.Id || queued.FlatteningID != row.FlatteningId || queued.StrategyID != row.StrategyInstanceId ||
		queued.State != domain.AdminFlattenQueued || queued.Revision != 1 || queued.OwnerID != "" || queued.OwnerEpoch != 0 || !queued.ClaimedAt.IsZero() {
		t.Fatal("queued marker invented ownership or lost exact identity", err)
	}
	for _, mutate := range []func(*modelv1.AdminAccountFlatteningTask){
		func(r *modelv1.AdminAccountFlatteningTask) { r.Id = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.FlatteningId = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.StrategyInstanceId = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.AccountId = "" },
		func(r *modelv1.AdminAccountFlatteningTask) { r.AccountId = "invalid account" },
		func(r *modelv1.AdminAccountFlatteningTask) { r.InstrumentId = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.CreateAt = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.UpdateAt = 999 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.DeleteAt = proto.Int64(1000) },
		func(r *modelv1.AdminAccountFlatteningTask) { r.Revision = 0 },
		func(r *modelv1.AdminAccountFlatteningTask) { r.State = "SUCCEEDED" },
		func(r *modelv1.AdminAccountFlatteningTask) { r.OwnerId = proto.String("private-owner") },
		func(r *modelv1.AdminAccountFlatteningTask) { r.OwnerEpoch = proto.Int64(1) },
		func(r *modelv1.AdminAccountFlatteningTask) { r.ClaimedAt = proto.Int64(1000) },
		func(r *modelv1.AdminAccountFlatteningTask) { r.State = string(domain.AdminFlattenActive) },
	} {
		bad := proto.CloneOf(row)
		mutate(bad)
		if got, err := adminFlattenProgress(bad); !errors.Is(err, domain.ErrAdminUnavailable) || got.ID != 0 {
			t.Fatal("corrupt progress returned partial identity or invented completion", err)
		}
	}
	row.State = string(domain.AdminFlattenActive)
	row.Revision = 9007199254741001
	row.OwnerId = proto.String("flatten-owner.0123456789abcdef0123456789abcdef")
	row.OwnerEpoch = proto.Int64(9007199254741003)
	row.ClaimedAt = proto.Int64(1000)
	active, err := adminFlattenProgress(row)
	if err != nil || active.State != domain.AdminFlattenActive || active.Revision != row.Revision || active.OwnerID != row.GetOwnerId() ||
		active.OwnerEpoch != row.GetOwnerEpoch() || active.ClaimedAt.UnixMilli() != row.GetClaimedAt() {
		t.Fatal("active progress lost stored owner or exact epoch", err)
	}
}
