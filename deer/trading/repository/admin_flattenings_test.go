package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestAdminFlatteningStoreBorrowsStorageAndRejectsCancellationBeforeIO(t *testing.T) {
	t.Parallel()
	if _, err := NewAdminAccountFlattenings(nil); !errors.Is(err, domain.ErrInvalidAdminFlatten) {
		t.Fatal("nil borrowed database was accepted", err)
	}
	store, err := NewAdminAccountFlattenings(&gorm.DB{})
	if err != nil {
		t.Fatal("constructor performed database IO", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	cancel()
	if _, err := store.Request(ctx, "", "", domain.AdminFlattenRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled flatten request reached storage", err)
	}
}

func TestAdminFlatteningFingerprintBindsEveryConfirmedBusinessField(t *testing.T) {
	t.Parallel()
	request := flattenRepositoryRequest()
	original := adminFlattenFingerprint(request)
	if len(original) != 64 {
		t.Fatal("flatten fingerprint is not bounded")
	}
	for _, mutate := range []func(*domain.AdminFlattenRequest){
		func(r *domain.AdminFlattenRequest) { r.StrategyID++ },
		func(r *domain.AdminFlattenRequest) { r.AccountID += "-other" },
		func(r *domain.AdminFlattenRequest) { r.InstrumentID++ },
		func(r *domain.AdminFlattenRequest) { r.CancellationID++ },
		func(r *domain.AdminFlattenRequest) { r.ExpectedRiskRevision++ },
		func(r *domain.AdminFlattenRequest) { r.ExpectedPortfolioRevision++ },
		func(r *domain.AdminFlattenRequest) { r.ExpectedBaseRevision++ },
		func(r *domain.AdminFlattenRequest) { r.Scope = "strategy" },
		func(r *domain.AdminFlattenRequest) { r.ConfirmedQuantity = decimal.NewFromInt(9) },
		func(r *domain.AdminFlattenRequest) { r.LimitPrice = decimal.NewFromInt(11) },
		func(r *domain.AdminFlattenRequest) { r.Reason += " changed" },
	} {
		candidate := request
		mutate(&candidate)
		if adminFlattenFingerprint(candidate) == original {
			t.Fatal("changed flatten confirmation retained the original fingerprint")
		}
	}
	request.ConfirmedQuantity = decimal.RequireFromString("10.000000000000000000000000000000000000")
	if adminFlattenFingerprint(request) != original {
		t.Fatal("equivalent ledger zeros changed business identity")
	}
}

func TestAdminFlatteningReceiptKeepsExactValuesAndRejectsCorruptStorage(t *testing.T) {
	t.Parallel()
	r := flattenRepositoryRequest()
	row := &executionmodelv1.AdminAccountFlattening{
		Id:                        9007199254740993,
		CreateAt:                  1000,
		UpdateAt:                  1000,
		ActorUserId:               9007199254740995,
		StrategyInstanceId:        r.StrategyID,
		AccountId:                 r.AccountID,
		InstrumentId:              r.InstrumentID,
		EmergencyCancellationId:   r.CancellationID,
		IdempotencyKey:            r.Key,
		Scope:                     string(r.Scope),
		ExpectedRiskRevision:      r.ExpectedRiskRevision,
		ExpectedPortfolioRevision: r.ExpectedPortfolioRevision,
		ExpectedBaseRevision:      r.ExpectedBaseRevision,
		ConfirmedQuantity:         "10.000000000000000000000000000000000000",
		LimitPrice:                "10.010000000000000000",
		Reason:                    r.Reason,
	}
	receipt, err := adminFlattenReceipt(row)
	if err != nil || receipt.ID != row.Id || receipt.ActorUserID != row.ActorUserId || receipt.StrategyID != r.StrategyID ||
		receipt.ExpectedRiskRevision != r.ExpectedRiskRevision || !receipt.ConfirmedQuantity.Equal(r.ConfirmedQuantity) || !receipt.LimitPrice.Equal(r.LimitPrice) || receipt.CreatedAt.UnixMilli() != row.CreateAt {
		t.Fatal("flatten receipt lost exact stored identity or decimal", err)
	}
	row.ConfirmedQuantity = "private-invalid-numeric"
	if receipt, err := adminFlattenReceipt(row); !errors.Is(err, domain.ErrAdminUnavailable) || receipt.ID != 0 || strings.Contains(err.Error(), "private") {
		t.Fatal("corrupt storage returned a partial or private receipt", err)
	}
}

func flattenRepositoryRequest() domain.AdminFlattenRequest {
	return domain.AdminFlattenRequest{
		Key:                       "flatten-repository-request-0001",
		Scope:                     domain.AdminFlattenScopeAccountInstrument,
		StrategyID:                9007199254740997,
		AccountID:                 "isolated-simulation-account",
		InstrumentID:              9007199254740999,
		CancellationID:            9007199254741001,
		ExpectedRiskRevision:      9007199254741003,
		ExpectedPortfolioRevision: 9007199254741005,
		ExpectedBaseRevision:      9007199254741007,
		ConfirmedQuantity:         decimal.NewFromInt(10),
		LimitPrice:                decimal.RequireFromString("10.01"),
		Reason:                    "explicit isolated account confirmation",
	}
}
