//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestRiskGrantCannotAuthorizeInvalidSelectedMarket(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	feed, token, snapshots, current := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	invalid := marketReadingFixture(feed, current.Reading.Sequence+1, "100")
	invalid.Quality, invalid.Reason = domain.MarketQualityInvalid, domain.MarketReasonSequence
	invalid.Bid, invalid.Ask, invalid.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	invalid.BidQuantity, invalid.AskQuantity = decimal.Zero, decimal.Zero
	if _, err := snapshots.Publish(ctx, token, current.Revision, invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("current invalid market escaped fresh risk flag: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func admissionSelectedMarket(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64) (domain.MarketFeedDefinition, domain.LeaseToken, domain.MarketRepository, domain.MarketPublication) {
	t.Helper()
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	policy := marketquality.DefaultPolicy()
	policy.MaximumAge = 5 * time.Second
	feed, err := feeds.Create(ctx, domain.MarketFeedDefinition{
		Source:        domain.MarketSourceBinance,
		InstrumentID:  instrumentID,
		Symbol:        "BTCUSDT",
		Mode:          domain.MarketModeReplay,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feed.ID,
	}
	var lease modelv1.MarketPublisherLease
	err = db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).First(&lease).Error
	token := domain.LeaseToken{
		Key:     key,
		OwnerID: lease.OwnerId,
		Epoch:   lease.Epoch,
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		leases, err := repository.NewLeases(db)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := ownerid.New(ctx, "admission-market-fixture")
		if err != nil {
			t.Fatal(err)
		}
		token, err = leases.Acquire(ctx, key, owner, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	current, err := snapshots.Get(ctx, feed.ID)
	if errors.Is(err, domain.ErrMarketMissing) {
		reading := marketReadingFixture(feed, 1, "100")
		reading.ValidUntil = reading.ReceivedAt.Add(4 * time.Second)
		current, err = snapshots.Publish(ctx, token, 0, reading)
	}
	if err != nil {
		t.Fatal(err)
	}
	return feed, token, snapshots, current
}

func TestCurrentMarketGateRejectsGrantProvenanceAndPermitsNewerSameOwner(t *testing.T) {
	for _, change := range []string{"expired-owner", "old-epoch", "future-revision", "wrong-source-version", "older-than-grant-age", "wrong-mode"} {
		t.Run(change, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			store, submit := admissionFixture(ctx, t, db)
			feed, token, snapshots, current := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
			next, err := snapshots.Publish(ctx, token, current.Revision, marketReadingFixture(feed, current.Reading.Sequence+1, "100.01"))
			if err != nil {
				t.Fatal(err)
			}
			// A later healthy revision from the same publisher must not invalidate
			// every in-flight grant merely because the market updates at 100 ms.
			accepted, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
			if err != nil || accepted.ID <= 0 {
				t.Fatalf("newer same-owner publication rejected: %v", err)
			}
			input := submit.Input
			input.IdempotencyKey += "-after-change"
			input.Quantity = decimal.NewFromInt(1)
			query := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", input.Authority.StrategyID)
			switch change {
			case "expired-owner":
				expireLeaseFixture(ctx, t, db, token.Key)
			case "old-epoch":
				err = query.UpdateColumn("market_epoch", token.Epoch+1).Error
			case "future-revision":
				err = query.UpdateColumn("market_revision", next.Revision+1).Error
			case "wrong-source-version":
				err = query.UpdateColumn("market_source_version", feed.SourceVersion+1).Error
			case "wrong-mode":
				live := liveAdmissionMarket(ctx, t, db, feed)
				err = query.UpdateColumn("market_feed_id", live.FeedID).Error
			case "older-than-grant-age":
				err = query.UpdateColumn("market_maximum_age_ms", 1).Error
				if err != nil {
					t.Fatal(err)
				}
				// Wait for a measured DB age, never manufacture an expired HEALTHY row.
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for {
					var clock struct{ AgeMS int64 }
					err = db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ?", feed.ID).Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint - event_at AS age_ms").Take(&clock).Error
					if err != nil || clock.AgeMS >= 1 {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-tick.C:
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Accept(ctx, input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
				t.Fatalf("%s escaped current market gate: %v", change, err)
			}
			assertReservedBudget(ctx, t, db, input.AccountID, "200")
		})
	}
}

func liveAdmissionMarket(ctx context.Context, t *testing.T, db *gorm.DB, replay domain.MarketFeedDefinition) domain.MarketReference {
	t.Helper()
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	replay.ID, replay.Mode = 0, domain.MarketModeLive
	feed, err := feeds.Create(ctx, replay)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "live-risk-fixture")
	if err != nil {
		t.Fatal(err)
	}
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feed.ID,
	}, owner, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := snapshots.Publish(ctx, token, 0, marketReadingFixture(feed, 1, "100"))
	if err != nil {
		t.Fatal(err)
	}
	return publication.MarketReference
}

func TestFinalSelectedMarketFenceRollsBackAlreadyStagedFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	feed, _, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	const callbackName = "muskdeer:test:expire-market-after-staging"
	if err := db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
			return
		}
		err := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).UpdateColumn("expires_at", 0).Error
		tx.Error = errors.Join(tx.Error, err)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	service := intentServiceFixture(t, store, core)
	rejected, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || rejected.State != domain.OrderStateRejected || rejected.ErrorCode != domain.IntentErrorRiskDenied {
		t.Fatalf("selected market loss committed staged order: %+v, %v", rejected, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestSelectedMarketModeCannotBeChangedInPlace(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("market_mode", string(domain.MarketModeLive)).Error
	assertDatabaseCode(t, err, "23514")
}

func TestMarketProvenanceUpgradeClosesLegacyGrants(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:12]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	strategy := identityInstance(instrument.Id, "legacy-risk-upgrade", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Omit("MarketMode", "RuntimeOwnerId", "RuntimeOwnerEpoch").Create(strategy).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = strategy.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	risk := &modelv1.RiskSnapshot{
		StrategyInstanceId: strategy.Id,
		AccountId:          strategy.AccountId,
		InstrumentId:       instrument.Id,
		ConfigVersion:      1,
		MarketDataSource:   string(domain.MarketSourceBinance),
		MarketEpoch:        1,
		MarketSequence:     1,
		ObservedAt:         now,
		ExpiresAt:          now + 5000,
		Reconciled:         true,
		AllowNewRisk:       true,
		Revision:           1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Omit("MarketFeedId", "MarketRevision", "MarketSourceVersion", "MarketMaximumAgeMs", "ReasonCode").Create(risk).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var stored modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).First(&stored, risk.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.AllowNewRisk || stored.MarketFeedId != nil || stored.Revision != 2 {
		t.Fatalf("legacy grant acquired fabricated provenance: %v", &stored)
	}
	err = db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", stored.Id).UpdateColumn("allow_new_risk", true).Error
	assertDatabaseCode(t, err, "23514")
}
