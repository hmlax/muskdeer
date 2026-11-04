package repository

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func adminFlattenFingerprint(r domain.AdminFlattenRequest) string {
	var data []byte
	for _, value := range []int64{r.StrategyID, r.InstrumentID, r.CancellationID, r.ExpectedRiskRevision, r.ExpectedPortfolioRevision, r.ExpectedBaseRevision} {
		data = binary.BigEndian.AppendUint64(data, uint64(value))
	}
	for _, value := range []string{domain.AdminFlattenAction, string(r.Scope), r.AccountID, r.ConfirmedQuantity.String(), r.LimitPrice.String(), r.Reason} {
		data = binary.BigEndian.AppendUint32(data, uint32(len(value)))
		data = append(data, value...)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func adminFlattenReceipt(row *executionmodelv1.AdminAccountFlattening) (domain.AdminFlattenReceipt, error) {
	if row == nil || row.Id <= 0 || row.ActorUserId <= 0 || row.CreateAt <= 0 || row.DeleteAt != nil {
		return domain.AdminFlattenReceipt{}, domain.ErrAdminUnavailable
	}
	quantity, qErr := domain.ParseLedgerDecimal(row.ConfirmedQuantity, false, true)
	price, pErr := domain.ParseDecimal(row.LimitPrice, domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	})
	if qErr != nil || pErr != nil {
		return domain.AdminFlattenReceipt{}, domain.ErrAdminUnavailable
	}
	r := domain.AdminFlattenRequest{
		Key:                       row.IdempotencyKey,
		Scope:                     domain.AdminFlattenScope(row.Scope),
		StrategyID:                row.StrategyInstanceId,
		AccountID:                 row.AccountId,
		InstrumentID:              row.InstrumentId,
		CancellationID:            row.EmergencyCancellationId,
		ExpectedRiskRevision:      row.ExpectedRiskRevision,
		ExpectedPortfolioRevision: row.ExpectedPortfolioRevision,
		ExpectedBaseRevision:      row.ExpectedBaseRevision,
		ConfirmedQuantity:         quantity,
		LimitPrice:                price,
		Reason:                    row.Reason,
	}
	if normalized, err := r.Normalize(); err != nil || normalized.Reason != row.Reason {
		return domain.AdminFlattenReceipt{}, domain.ErrAdminUnavailable
	}
	return domain.AdminFlattenReceipt{
		ID:                        row.Id,
		ActorUserID:               row.ActorUserId,
		StrategyID:                r.StrategyID,
		AccountID:                 r.AccountID,
		InstrumentID:              r.InstrumentID,
		CancellationID:            r.CancellationID,
		Scope:                     r.Scope,
		ExpectedRiskRevision:      r.ExpectedRiskRevision,
		ExpectedPortfolioRevision: r.ExpectedPortfolioRevision,
		ExpectedBaseRevision:      r.ExpectedBaseRevision,
		ConfirmedQuantity:         r.ConfirmedQuantity,
		LimitPrice:                r.LimitPrice,
		Reason:                    r.Reason,
		CreatedAt:                 time.UnixMilli(row.CreateAt).UTC(),
	}, nil
}
