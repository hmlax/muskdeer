//go:build integration

package console

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

func testDatabase(ctx context.Context, t *testing.T) *gorm.DB {
	t.Helper()
	database := os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE")
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || !strings.HasPrefix(database, "muskdeer_test_") {
		t.Fatal("isolated PostgreSQL test database required")
	}
	connection, err := postgres.New(ctx, &config.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         database,
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           "public",
		MaxOpenConns:     4,
		MaxIdleConns:     2,
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(5 * time.Second),
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(time.Minute),
	}, "current-state-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	db := connection.GORM()
	if !db.WithContext(ctx).Migrator().HasTable(&model.SchemaMigration{}) {
		if err := schemaformat.Install(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestManagementScopeCASAndStoppedStartup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db := testDatabase(ctx, t)
	product := &model.Instrument{
		Symbol:          "BTCUSDT",
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		PriceTick:       "0.1",
		QuantityStep:    "0.001",
		MinNotional:     "1",
		Status:          domain.InstrumentStatusTrading,
		ExecutionVenue:  native.Venue,
		MetadataVersion: 1,
	}
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Create(product).Error; err != nil {
		t.Fatal(err)
	}
	secondProduct := proto.CloneOf(product)
	secondProduct.Id = 0
	secondProduct.Symbol = "ETHUSDT"
	secondProduct.BaseAsset = "ETH"
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Create(secondProduct).Error; err != nil {
		t.Fatal(err)
	}
	products := []*model.Instrument{product, secondProduct}
	accounts := []string{native.ScopeKey(10001, 10001), native.ScopeKey(10002, 10002)}
	for index, account := range accounts {
		if err := db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Create(&model.ExecutionShardLease{AccountId: account}).Error; err != nil {
			t.Fatal(err)
		}
		budget := &model.TradingAccountPolicy{
			AccountId:            account,
			UserId:               int64(10001 + index),
			FundId:               int64(10001 + index),
			Symbol:               products[index].Symbol,
			MarketDataSource:     native.Source,
			PriceTick:            product.PriceTick,
			QuantityStep:         product.QuantityStep,
			Leverage:             10,
			MaximumNotional:      "1000",
			MaximumOrderNotional: "100",
			ObservedNotional:     "0",
			ReservedNotional:     "0",
			AvailableQuote:       "1000",
			WorstFeeRate:         "0.001",
			Enabled:              true,
			CutoverCompleted:     true,
		}
		if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(budget).Error; err != nil {
			t.Fatal(err)
		}
	}
	parameters := &common.StrategyInstanceParameters{
		SchemaVersion: 1,
		Execution: &common.StrategyExecutionParameters{
			PollIntervalMs:     200,
			OperationTimeoutMs: 2000,
			LeaseTtlMs:         10000,
			Leverage:           10,
		},
		MarketMaker: &common.DepthMakerParameters{
			Depth:               20,
			QuantityFraction:    "0.1",
			ReplacementFraction: "0.1",
			BridgeSlots:         1,
		},
	}
	var rows []*strategy.StrategyInstance
	for index, account := range accounts {
		row, err := instanceconfig.Manage(ctx, db, instanceconfig.ManagementWrite{
			Operation: instanceconfig.ManagementCreate,
			Subject:   "user:1",
			RequestID: strings.Repeat("a", 20),
			Input: &common.StrategyInstanceInput{
				InstanceKey:      fmtKey(index),
				TemplateId:       strategyregistry.MakerTemplateID,
				TemplateRevision: 1,
				AccountId:        account,
				InstrumentId:     products[index].Id,
				MarketDataSource: native.Source,
				Parameters:       proto.CloneOf(parameters),
			},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	active, err := instanceconfig.Load(ctx, db)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%d err=%v", len(active), err)
	}
	all, err := instanceconfig.LoadAll(ctx, db)
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	server := &Server{db: db}
	actor := &pb.Actor{
		Subject:           "user:2",
		TenantId:          7,
		AllowedAccountIds: []string{accounts[0]},
	}
	operator := operatorContext(ctx, rpcsecurity.ServiceOperator)
	page, err := server.ListAccounts(operator, &pb.ListRequest{Actor: actor})
	if err != nil || page.GetTotal() != 1 || page.Items[0].AccountId != accounts[0] {
		t.Fatalf("scoped accounts=%v err=%v", page, err)
	}
	empty, err := server.ListAccounts(operator, &pb.ListRequest{
		Actor:     actor,
		AccountId: accounts[1],
	})
	if err != nil || empty.GetTotal() != 0 {
		t.Fatalf("foreign exact account escaped scope: %v %v", empty, err)
	}
	if _, err := server.row(operator, actor, rows[1].Id); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign instance=%v", err)
	}
	verifyManagementTransitions(ctx, t, db, rows, accounts, parameters)
}
func verifyManagementTransitions(ctx context.Context, t *testing.T, db *gorm.DB, rows []*strategy.StrategyInstance, accounts []string, parameters *common.StrategyInstanceParameters) {
	t.Helper()
	command := instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                "user:2",
		RequestID:              strings.Repeat("b", 20),
		ID:                     rows[0].Id,
		ExpectedRevision:       1,
		ExpectedControlVersion: 1,
		Accounts:               []string{accounts[0]},
	}
	var group sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		group.Go(func() { _, err := instanceconfig.Manage(ctx, db, command, nil); outcomes <- err })
	}
	group.Wait()
	close(outcomes)
	success, conflicts := 0, 0
	for err := range outcomes {
		switch {
		case err == nil:
			success++
		case errors.Is(err, instanceconfig.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	if ok, err := instanceconfig.Active(ctx, db, rows[0].Id, 1); err != nil || !ok {
		t.Fatalf("started current revision not admitted: %v", err)
	}
	command.Operation = instanceconfig.ManagementStop
	command.ExpectedControlVersion = 2
	stopped, err := instanceconfig.Manage(ctx, db, command, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Revision != 1 || stopped.DesiredRunning || stopped.RequestedConfigVersion != 3 {
		t.Fatalf("stop lost config identity: %v", stopped)
	}
	if ok, err := instanceconfig.Active(ctx, db, rows[0].Id, 1); err != nil || ok {
		t.Fatalf("stopped instance admitted: %v", err)
	}
	updated := proto.CloneOf(parameters)
	updated.MarketMaker.Depth = 10
	command.Operation = instanceconfig.ManagementUpdate
	command.ExpectedControlVersion = 3
	command.Input = &common.StrategyInstanceInput{Parameters: updated}
	changed, err := instanceconfig.Manage(ctx, db, command, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision != 2 || changed.Parameters.ManagementChange != nil {
		t.Fatal("current config contaminated by historical audit")
	}
	var history []*strategy.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Where("instance_id = ?", rows[0].Id).Order("version").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[3].Parameters.GetManagementChange().GetOperation() != instanceconfig.ManagementUpdate || history[1].CreatedBy != "user:2" {
		t.Fatalf("incomplete management audit: %v", history)
	}
	command.ID = rows[1].Id
	command.ExpectedRevision = 1
	command.ExpectedControlVersion = 1
	if _, err := instanceconfig.Manage(ctx, db, command, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign account mutation=%v", err)
	}
}
func fmtKey(index int) string { return "console-maker-" + strconv.Itoa(index) }
