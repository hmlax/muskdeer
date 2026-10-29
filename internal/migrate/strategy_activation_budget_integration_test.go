//go:build integration

package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyActivationBudgetFollowsBothTemplatesOnlyAfterActivation(t *testing.T) {
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		t.Run(string(template), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, creator, submit := adminCreationFixture(ctx, t, db)
			request := adminCreationRequest(t, submit, template, domain.MarketSourceBinance)
			if request.Parameters.Maker != nil {
				request.Parameters.Maker.InventoryHardLimit = decimal.NewFromInt(20)
			} else {
				request.Parameters.Grid.InventoryLimit = decimal.NewFromInt(20)
			}
			receipt, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
			if err != nil {
				t.Fatal(err)
			}
			token := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
				Kind:       domain.LeaseKindStrategy,
				StrategyID: receipt.StrategyID,
			})
			lifecycle, err := repository.NewStrategyLifecycle(db)
			if err != nil {
				t.Fatal(err)
			}
			current, err := lifecycle.Instance(ctx, receipt.StrategyID)
			if err != nil {
				t.Fatal(err)
			}
			current, err = lifecycle.Transition(ctx, current, token, domain.StrategyTransition{
				State:             domain.StrategyStateRecovering,
				ActivateRequested: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", current.ID).UpdateColumn("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
			before := activationBudget(ctx, t, db, current.ID)
			version := stageActivationConfig(ctx, t, db, current.ID, domain.MarketSourceOKX, "20", "35.000000000000000003")
			if retained := activationBudget(ctx, t, db, current.ID); !proto.Equal(before, retained) {
				t.Fatal("publishing requested parameters changed the active budget")
			}
			current, err = lifecycle.Instance(ctx, current.ID)
			if err != nil {
				t.Fatal(err)
			}
			activated, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{
				State:             domain.StrategyStateRecovering,
				ActivateRequested: true,
			})
			if err != nil || activated.ActiveVersion != version.Version || activated.ActiveSource != domain.MarketSourceOKX || activated.DesiredState != domain.StrategyStateStopped {
				t.Fatalf("activation changed the target or missed the requested pair: %+v, %v", activated, err)
			}
			want := "35.000000000000000003"
			if template == domain.StrategyTemplateMarketMaker {
				want = "70.000000000000000006"
			}
			after := activationBudget(ctx, t, db, current.ID)
			if !decimal.RequireFromString(after.LimitNotional).Equal(decimal.RequireFromString(want)) || after.Revision != before.Revision+1 || after.ReservedNotional != before.ReservedNotional || after.Enabled != before.Enabled {
				t.Fatalf("active budget did not follow exact typed parameters: %v", after)
			}
			if _, err := lifecycle.Transition(ctx, activated, token, domain.StrategyTransition{
				State:             domain.StrategyStateRecovering,
				ActivateRequested: true,
			}); err != nil {
				t.Fatal(err)
			}
			if retained := activationBudget(ctx, t, db, current.ID); !proto.Equal(after, retained) {
				t.Fatal("replaying an active pair changed budget revision")
			}
			var account modelv1.AccountBudget
			if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", submit.Input.AccountID).First(&account).Error; err != nil {
				t.Fatal(err)
			}
			if !decimal.RequireFromString(account.LimitNotional).Equal(decimal.NewFromInt(300)) || !decimal.RequireFromString(account.ReservedNotional).IsZero() || account.Revision != 1 {
				t.Fatal("activation rewrote shared account capacity")
			}
		})
	}
}

func TestStrategyActivationBudgetWaitsForLiveAndUnknownCancellation(t *testing.T) {
	for _, state := range []domain.OrderState{domain.OrderStateLive, domain.OrderStateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			core, intents := executionFixture(t, db)
			submit.Input.Deadline = time.Now().Add(time.Second)
			live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
			if err != nil || live.State != domain.OrderStateLive {
				t.Fatalf("actual funded order did not become live: %+v, %v", live, err)
			}
			if state == domain.OrderStateUnknown {
				if unknown, err := intents.Unknown(ctx, live, domain.IntentErrorOutcomeUnknown); err != nil || unknown.State != state {
					t.Fatalf("lost observation did not retain unknown obligation: %+v, %v", unknown, err)
				}
			}
			strategyID := submit.Input.Authority.StrategyID
			stageActivationConfig(ctx, t, db, strategyID, domain.MarketSourceOKX, "20", "20.000000000000000002")
			before := activationBudget(ctx, t, db, strategyID)
			lifecycle, err := repository.NewStrategyLifecycle(db)
			if err != nil {
				t.Fatal(err)
			}
			current, err := lifecycle.Instance(ctx, strategyID)
			if err != nil {
				t.Fatal(err)
			}
			transition := domain.StrategyTransition{
				State:             domain.StrategyStateRecovering,
				ActivateRequested: true,
			}
			token := lifecycleToken(submit.Input.Authority)
			if _, err := lifecycle.Transition(ctx, current, token, transition); !errors.Is(err, domain.ErrOutcomeUnknown) {
				t.Fatalf("%s order allowed activation: %v", state, err)
			}
			assertActivationUnchanged(ctx, t, db, lifecycle, current, before)
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
			cancels := cancellationServiceFixture(t, db, intents, core)
			pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "config-budget-drain"), submit.ExecutionAuthority)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := lifecycle.Transition(ctx, current, token, transition); !errors.Is(err, domain.ErrOutcomeUnknown) {
				t.Fatalf("unconfirmed cancellation released active policy: %v", err)
			}
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
			if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
				t.Fatal(err)
			}
			if _, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil {
				t.Fatal(err)
			}
			activated, err := lifecycle.Transition(ctx, current, token, transition)
			if err != nil || activated.ActiveVersion != 2 || activated.ActiveSource != domain.MarketSourceOKX {
				t.Fatalf("confirmed cancellation did not permit activation: %+v, %v", activated, err)
			}
			after := activationBudget(ctx, t, db, strategyID)
			if !decimal.RequireFromString(after.LimitNotional).Equal(decimal.RequireFromString("40.000000000000000004")) || !decimal.RequireFromString(after.ReservedNotional).IsZero() {
				t.Fatal("confirmed cancellation did not install the exact new capacity")
			}
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
			assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
		})
	}
}

func TestStrategyActivationBudgetRejectsResidualReservation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	strategyID := submit.Input.Authority.StrategyID
	stageActivationConfig(ctx, t, db, strategyID, domain.MarketSourceOKX, "20", "20.000000000000000002")
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", strategyID).UpdateColumn("reserved_notional", "1").Error; err != nil {
		t.Fatal(err)
	}
	before := activationBudget(ctx, t, db, strategyID)
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.Instance(ctx, strategyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(ctx, current, lifecycleToken(submit.Input.Authority), domain.StrategyTransition{
		State:             domain.StrategyStateRecovering,
		ActivateRequested: true,
	}); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatalf("residual reservation allowed activation: %v", err)
	}
	assertActivationUnchanged(ctx, t, db, lifecycle, current, before)
}

func TestStrategyActivationBudgetDatabaseRejectsBypassedSynchronization(t *testing.T) {
	for _, mismatch := range []string{"limit", "reservation"} {
		t.Run(mismatch, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			strategyID := submit.Input.Authority.StrategyID
			stageActivationConfig(ctx, t, db, strategyID, domain.MarketSourceOKX, "20", "20.000000000000000002")
			lifecycle, err := repository.NewStrategyLifecycle(db)
			if err != nil {
				t.Fatal(err)
			}
			current, err := lifecycle.Instance(ctx, strategyID)
			if err != nil {
				t.Fatal(err)
			}
			current, err = lifecycle.Transition(ctx, current, lifecycleToken(submit.Input.Authority), domain.StrategyTransition{State: domain.StrategyStateRecovering})
			if err != nil {
				t.Fatal(err)
			}
			if mismatch == "reservation" {
				if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", strategyID).
					Updates(map[string]any{
						"limit_notional":    "40.000000000000000004",
						"reserved_notional": "1",
					}).Error; err != nil {
					t.Fatal(err)
				}
			}
			before := activationBudget(ctx, t, db, strategyID)
			err = db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).
				Select("ActiveConfigVersion", "ActiveMarketDataSource", "Revision").Updates(&modelv1.StrategyInstance{
				ActiveConfigVersion:    proto.Int64(2),
				ActiveMarketDataSource: proto.String(string(domain.MarketSourceOKX)),
				Revision:               current.Revision + 1,
			}).Error
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.ConstraintName != "strategy_activation_budget" {
				t.Fatalf("database accepted %s outside activation transaction: %v", mismatch, err)
			}
			assertActivationUnchanged(ctx, t, db, lifecycle, current, before)
		})
	}
}

func TestStrategyActivationBudgetRollsBackOnInventoryAndCommitFence(t *testing.T) {
	for _, failure := range []string{"inventory", "lease"} {
		t.Run(failure, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, submit := admissionFixture(ctx, t, db)
			strategyID := submit.Input.Authority.StrategyID
			inventory := "20"
			if failure == "inventory" {
				inventory = "9"
			}
			stageActivationConfig(ctx, t, db, strategyID, domain.MarketSourceOKX, inventory, "20.000000000000000002")
			before := activationBudget(ctx, t, db, strategyID)
			lifecycle, err := repository.NewStrategyLifecycle(db)
			if err != nil {
				t.Fatal(err)
			}
			current, err := lifecycle.Instance(ctx, strategyID)
			if err != nil {
				t.Fatal(err)
			}
			want := domain.ErrRiskDenied
			if failure == "lease" {
				want = domain.ErrLeaseLost
				const callback = "test:expire-strategy-budget-activation"
				if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeFor[modelv1.StrategyBudget]() {
						return
					}
					result := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.StrategyLease{}).
						Where("strategy_instance_id = ?", strategyID).UpdateColumn("expires_at", 0)
					if result.Error != nil {
						tx.Error = errors.Join(tx.Error, result.Error)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Callback().Update().Remove(callback); err != nil {
						t.Error(err)
					}
				})
			}
			if _, err := lifecycle.Transition(ctx, current, lifecycleToken(submit.Input.Authority), domain.StrategyTransition{
				State:             domain.StrategyStateRecovering,
				ActivateRequested: true,
			}); !errors.Is(err, want) {
				t.Fatalf("activation escaped %s check: %v", failure, err)
			}
			assertActivationUnchanged(ctx, t, db, lifecycle, current, before)
		})
	}
}

func stageActivationConfig(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64, source domain.MarketSource, inventory, budget string) *modelv1.StrategyConfigVersion {
	t.Helper()
	var previous modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ?", strategyID).Order("version DESC").First(&previous).Error; err != nil {
		t.Fatal(err)
	}
	version := proto.CloneOf(&previous)
	version.Id, version.CreateAt, version.UpdateAt, version.Version = 0, 0, 0, previous.Version+1
	version.MarketDataSource = string(source)
	if version.MakerParameters != nil {
		version.MakerParameters.SideBudget, version.MakerParameters.InventoryHardLimit = budget, inventory
	} else {
		version.GridParameters.TotalBudget, version.GridParameters.InventoryLimit = budget, inventory
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).
			Updates(map[string]any{
				"requested_config_version": version.Version,
				"market_data_source":       string(source),
				"revision":                 gorm.Expr("revision + 1"),
			}).Error
	}); err != nil {
		t.Fatal(err)
	}
	return version
}

func activationBudget(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64) *modelv1.StrategyBudget {
	t.Helper()
	var row modelv1.StrategyBudget
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", strategyID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}

func assertActivationUnchanged(ctx context.Context, t *testing.T, db *gorm.DB, lifecycle domain.StrategyLifecycle, before domain.StrategySpec, budget *modelv1.StrategyBudget) {
	t.Helper()
	retained, err := lifecycle.Instance(ctx, before.ID)
	if err != nil || retained != before || !proto.Equal(budget, activationBudget(ctx, t, db, before.ID)) {
		t.Fatalf("failed activation left partial configuration or budget: %+v, %v", retained, err)
	}
}
