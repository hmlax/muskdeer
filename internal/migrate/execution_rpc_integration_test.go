//go:build integration

package migrate

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/simulator"
	"github.com/hmlax/muskdeer/internal/testcert"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestExecutionRPCAuthenticatesThenSubmitsQueriesAndCancels(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	writer, reader := executionRPCFixture(ctx, t, db, core, intents, submit.ExecutionAuthority.OwnerID)
	input := rpcSubmitFixture(submit, "rpc-buy", commonv1.OrderSide_ORDER_SIDE_BUY, "99")
	spoofed := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-service-id", rpcsecurity.ServiceStrategy))
	if _, err := reader.SubmitIntent(spoofed, input); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("API identity forged write permission: %v", err)
	}
	input.Price = "1e2"
	if _, err := writer.SubmitIntent(ctx, input); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid decimal reached execution: %v", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
	input.Price = "99"
	buy, err := writer.SubmitIntent(ctx, input)
	if err != nil || buy.Order.State != commonv1.OrderState_ORDER_STATE_LIVE || buy.Order.Price != "99" || buy.Order.Quantity != "1" || buy.Order.CoreRevision <= 0 {
		t.Fatalf("typed SubmitIntent = %v, %v", buy, err)
	}
	input.Deadline = timestamppb.New(time.Now().Add(time.Second))
	duplicate, err := writer.SubmitIntent(ctx, input)
	if err != nil || duplicate.Order.IntentId != buy.Order.IntentId || duplicate.Order.CoreOrderId != buy.Order.CoreOrderId {
		t.Fatalf("RPC idempotent identity changed: %v, %v", duplicate, err)
	}
	input.Quantity = "2"
	if _, err := writer.SubmitIntent(ctx, input); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed business key was accepted: %v", err)
	}
	sell, err := writer.SubmitIntent(ctx, rpcSubmitFixture(submit, "rpc-sell", commonv1.OrderSide_ORDER_SIDE_SELL, "101"))
	if err != nil || sell.Order.State != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatalf("second RPC order = %v, %v", sell, err)
	}
	assertRPCPages(ctx, t, reader, submit.Input.AccountID, buy.Order.IntentId, sell.Order.IntentId)
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       buy.Order.ClientOrderId,
		TradeID:             "rpc-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(99),
		Quantity:            decimal.RequireFromString("0.5"),
	}); err != nil {
		t.Fatal(err)
	}
	partial, err := reader.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: submit.Input.AccountID,
		Lookup:    &executionv1.FindOrderRequest_ClientOrderId{ClientOrderId: buy.Order.ClientOrderId},
	})
	if err != nil || partial.Order.State != commonv1.OrderState_ORDER_STATE_PARTIALLY_FILLED || partial.Order.FilledQuantity != "0.5" {
		t.Fatalf("authoritative FindOrder = %v, %v", partial, err)
	}
	assertRPCFillAndAccount(ctx, t, reader, submit.Input)
	assertRPCCancellation(ctx, t, writer, reader, core, submit, sell.Order.ClientOrderId)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "49.5")
	open, err := reader.ListOrders(ctx, &executionv1.ListOrdersRequest{
		AccountId: submit.Input.AccountID,
		OpenOnly:  true,
	})
	if err != nil || len(open.Orders) != 1 || open.Orders[0].IntentId != buy.Order.IntentId {
		t.Fatalf("terminal order remained in open view: %v, %v", open, err)
	}
}

func TestExecutionRPCUnknownOutcomeQueriesWithoutResubmitting(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	lost, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit: simulator.ResponseLost,
		Cancel: simulator.ResponseNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := &observedGateway{TradingGateway: lost}
	writer, reader := executionRPCFixture(ctx, t, db, observed, intents, submit.ExecutionAuthority.OwnerID)
	unknown, err := writer.SubmitIntent(ctx, rpcSubmitFixture(submit, "rpc-unknown", commonv1.OrderSide_ORDER_SIDE_BUY, "99"))
	if err != nil || unknown.Order.State != commonv1.OrderState_ORDER_STATE_UNKNOWN || unknown.Order.CoreOrderId != "" || observed.submits.Load() != 1 {
		t.Fatalf("RPC lost submit outcome = %v, %v", unknown, err)
	}
	confirmed, err := reader.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: submit.Input.AccountID,
		Lookup:    &executionv1.FindOrderRequest_IntentId{IntentId: unknown.Order.IntentId},
	})
	if err != nil || confirmed.Order.State != commonv1.OrderState_ORDER_STATE_LIVE || confirmed.Order.CoreOrderId == "" || observed.submits.Load() != 1 || observed.queries.Load() != 1 {
		t.Fatalf("RPC query-first recovery = %v, %v", confirmed, err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "99")
}

func TestExecutionRPCDefiniteSelfTradeRejectionReleasesIntentReservation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	writer, _ := executionRPCFixture(ctx, t, db, core, intents, submit.ExecutionAuthority.OwnerID)
	ask, err := writer.SubmitIntent(ctx, rpcSubmitFixture(submit, "rpc-stp-ask", commonv1.OrderSide_ORDER_SIDE_SELL, "101"))
	if err != nil || ask.Order.State != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatalf("STP source ask = %v, %v", ask, err)
	}
	rejected, err := writer.SubmitIntent(ctx, rpcSubmitFixture(submit, "rpc-stp-cross", commonv1.OrderSide_ORDER_SIDE_BUY, "101"))
	if err != nil || rejected.Order.State != commonv1.OrderState_ORDER_STATE_REJECTED || rejected.Order.ErrorCode != domain.IntentErrorCoreRejected {
		t.Fatalf("definite STP rejection retained an unknown intent: %v, %v", rejected, err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "101")
}

func assertRPCPages(ctx context.Context, t *testing.T, client executionv1.ExecutionClient, account string, firstID, secondID int64) {
	t.Helper()
	first, err := client.ListOrders(ctx, &executionv1.ListOrdersRequest{
		AccountId: account,
		PageSize:  1,
	})
	if err != nil || len(first.Orders) != 1 || first.Orders[0].IntentId != firstID || first.NextAfterId != firstID {
		t.Fatalf("first keyset page = %v, %v", first, err)
	}
	second, err := client.ListOrders(ctx, &executionv1.ListOrdersRequest{
		AccountId: account,
		PageSize:  1,
		AfterId:   first.NextAfterId,
	})
	if err != nil || len(second.Orders) != 1 || second.Orders[0].IntentId != secondID || second.NextAfterId != 0 {
		t.Fatalf("second keyset page = %v, %v", second, err)
	}
	if _, err := client.ListOrders(ctx, &executionv1.ListOrdersRequest{
		AccountId: account,
		PageSize:  domain.MaximumExecutionPage + 1,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unbounded page accepted: %v", err)
	}
	other, err := client.ListOrders(ctx, &executionv1.ListOrdersRequest{AccountId: "other-account"})
	if err != nil || len(other.Orders) != 0 {
		t.Fatalf("other account exposed original orders: %v, %v", other, err)
	}
	if _, err := client.FindOrder(ctx, &executionv1.FindOrderRequest{
		AccountId: "other-account",
		Lookup:    &executionv1.FindOrderRequest_IntentId{IntentId: firstID},
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("other account resolved original order: %v", err)
	}
}

func assertRPCFillAndAccount(ctx context.Context, t *testing.T, client executionv1.ExecutionClient, input domain.OrderInput) {
	t.Helper()
	fills, err := client.ListFills(ctx, &executionv1.ListFillsRequest{
		AccountId:          input.AccountID,
		StrategyInstanceId: input.Authority.StrategyID,
		InstrumentId:       input.InstrumentID,
	})
	if err != nil || len(fills.Fills) != 1 || fills.Fills[0].Price != "99" || fills.Fills[0].Quantity != "0.5" || fills.Fills[0].QuoteNotional != "49.5" || fills.Fills[0].FeeAmount != "0.0495" || fills.Fills[0].Sequence <= 0 {
		t.Fatalf("typed ListFills = %v, %v", fills, err)
	}
	account, err := client.GetAccount(ctx, &executionv1.GetAccountRequest{AccountId: input.AccountID})
	if err != nil || account.Sequence <= 1 || account.ObservedAt.CheckValid() != nil || len(account.Balances) != 2 {
		t.Fatalf("consistent GetAccount = %v, %v", account, err)
	}
	base, quote := account.Balances[0], account.Balances[1]
	if base.Asset != "BTC" || base.Available != "9.5" || base.Frozen != "1" || quote.Asset != "USDT" || quote.Available != "900.901" || quote.Frozen != "49.5495" {
		t.Fatalf("account revision mixed balance states: %v", account)
	}
	other, err := client.ListFills(ctx, &executionv1.ListFillsRequest{AccountId: "other-account"})
	if err != nil || len(other.Fills) != 0 {
		t.Fatalf("other account exposed original fills: %v, %v", other, err)
	}
}

func assertRPCCancellation(ctx context.Context, t *testing.T, writer, reader executionv1.ExecutionClient, core domain.Simulator, submit domain.GatewaySubmit, clientID string) {
	t.Helper()
	pending, err := writer.CancelOrder(ctx, &executionv1.CancelOrderRequest{
		IdempotencyKey: "rpc-cancel",
		Authority:      rpcAuthority(submit.Input.Authority),
		AccountId:      submit.Input.AccountID,
		InstrumentId:   submit.Input.InstrumentID,
		ClientOrderId:  clientID,
		Deadline:       timestamppb.New(time.Now().Add(time.Second)),
	})
	if err != nil || pending.Cancellation == nil || pending.Cancellation.Id <= 0 || pending.Cancellation.State != commonv1.CancellationState_CANCELLATION_STATE_PENDING || pending.Order.State != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatalf("typed cancellation request = %v, %v", pending, err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, clientID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := reader.FindCancellation(ctx, &executionv1.FindCancellationRequest{
		AccountId:      submit.Input.AccountID,
		CancellationId: pending.Cancellation.Id,
	})
	if err != nil || confirmed.Cancellation.State != commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED || confirmed.Order.State != commonv1.OrderState_ORDER_STATE_CANCELED {
		t.Fatalf("cancellation request/order outcomes merged: %v, %v", confirmed, err)
	}
}

func rpcSubmitFixture(submit domain.GatewaySubmit, key string, side commonv1.OrderSide, price string) *executionv1.SubmitIntentRequest {
	return &executionv1.SubmitIntentRequest{
		IdempotencyKey: key,
		Authority:      rpcAuthority(submit.Input.Authority),
		AccountId:      submit.Input.AccountID,
		InstrumentId:   submit.Input.InstrumentID,
		Side:           side,
		Price:          price,
		Quantity:       "1",
		TimeInForce:    commonv1.TimeInForce_TIME_IN_FORCE_POST_ONLY,
		Deadline:       timestamppb.New(time.Now().Add(time.Second)),
	}
}

func rpcAuthority(authority domain.StrategyAuthority) *executionv1.StrategyAuthority {
	return &executionv1.StrategyAuthority{
		StrategyInstanceId: authority.StrategyID,
		ConfigVersion:      authority.ConfigVersion,
		OwnerId:            authority.OwnerID,
		OwnerEpoch:         authority.OwnerEpoch,
	}
}

func executionRPCFixture(ctx context.Context, t *testing.T, db *gorm.DB, core domain.TradingGateway, intents domain.IntentExecutionRepository, incarnation string) (executionv1.ExecutionClient, executionv1.ExecutionClient) {
	t.Helper()
	queries, err := repository.NewExecutionQueries(db)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := usecase.NewExecutionOwner(leases, incarnation, domain.MaxLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := executiongrpc.New(executiongrpc.Dependencies{
		Intents:       intentServiceFixture(t, intents, core),
		IntentStore:   intents,
		Cancellations: cancellationServiceFixture(t, db, intents, core),
		Queries:       queries,
		Authority:     ownership,
	})
	if err != nil {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	cfg := &configv1.ServiceConfig{
		Name:            "execution",
		InstanceId:      "rpc-execution",
		Version:         "test",
		StartupTimeout:  durationpb.New(3 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		GrpcSecurity: &configv1.GRPCServerSecurity{
			Mtls:                ca.Issue(t, rpcsecurity.ServiceExecution, false),
			AllowedPeerServices: []string{rpcsecurity.ServiceStrategy, rpcsecurity.ServiceAPI, rpcsecurity.ServicePortfolio},
		},
	}
	server, cleanup, err := deer.NewDeer(ctx, cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
			executionv1.RegisterExecutionServer(registrar, handler)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, cancelRun := context.WithCancel(context.WithoutCancel(t.Context()))
	finished := make(chan error, 1)
	go func() { finished <- server.Run(run) }()
	t.Cleanup(func() {
		cancelRun()
		stop, cancelStop := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
		defer cancelStop()
		if err := cleanup(stop); err != nil {
			t.Error(err)
		}
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	return executionRPCClient(t, ca, server.GRPCAddress(), rpcsecurity.ServiceStrategy), executionRPCClient(t, ca, server.GRPCAddress(), rpcsecurity.ServiceAPI)
}

func executionRPCClient(t *testing.T, ca *testcert.Authority, address, service string) executionv1.ExecutionClient {
	t.Helper()
	identity, err := rpcsecurity.Client(ca.Issue(t, service, false), service, rpcsecurity.ServiceExecution)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(credentials.NewTLS(identity)), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	return executionv1.NewExecutionClient(connection)
}
