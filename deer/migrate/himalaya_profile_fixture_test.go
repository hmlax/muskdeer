//go:build integration

package migrate

import (
	"context"
	"testing"

	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func configureNativeProfileFixture(ctx context.Context, t *testing.T, db *gorm.DB, request himalaya.SubmitRequest) {
	t.Helper()
	row := &commonmodelv1.TradingAccountPolicy{
		Symbol:           request.Symbol,
		MarketDataSource: native.Source,
		PriceTick:        "0.1",
		QuantityStep:     "0.001",
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", native.ScopeKey(request.UserID, request.FundID)).
		Select("Symbol", "MarketDataSource", "PriceTick", "QuantityStep").Updates(row).Error; err != nil {
		t.Fatal(err)
	}
}
