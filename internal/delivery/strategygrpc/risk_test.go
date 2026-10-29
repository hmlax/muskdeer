package strategygrpc

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type riskRPCStore struct {
	calls      int
	token      string
	csrf       string
	request    domain.AdminRiskRequest
	err        error
	badState   bool
	badReceipt bool
}

func (s *riskRPCStore) State(_ context.Context, token string, id int64) (domain.AdminRiskState, error) {
	s.calls++
	s.token = token
	if s.badState {
		id++
	}
	return domain.AdminRiskState{
		StrategyID:             id,
		Enabled:                false,
		Revision:               9007199254740993,
		QuoteAsset:             "USDT",
		LimitNotional:          decimal.RequireFromString("300.000000000000000001"),
		ReservedNotional:       decimal.RequireFromString("123.000000000000000001"),
		State:                  domain.StrategyStateRunning,
		DesiredState:           domain.StrategyStatePaused,
		RequestedConfigVersion: 9007199254740995,
		Source:                 domain.MarketSourceOKX,
		UpdatedAt:              time.UnixMilli(1700000000000),
	}, s.err
}

func (s *riskRPCStore) Change(_ context.Context, token, csrf string, request domain.AdminRiskRequest) (domain.AdminRiskReceipt, error) {
	s.calls++
	s.token, s.csrf, s.request = token, csrf, request
	id := request.StrategyID
	if s.badReceipt {
		id++
	}
	return domain.AdminRiskReceipt{
		ID:                    9007199254740993,
		ActorUserID:           1,
		StrategyID:            id,
		Action:                request.Action,
		PreviousEnabled:       true,
		Enabled:               request.Action == domain.AdminRiskUnblock,
		PreviousRevision:      request.ExpectedRevision,
		Revision:              request.ExpectedRevision + 1,
		ExpectedConfigVersion: request.ExpectedConfigVersion,
		ExpectedDesiredState:  request.ExpectedDesiredState,
		Reason:                request.Reason,
		CreatedAt:             time.UnixMilli(1700000000000),
	}, s.err
}

func riskRPCRequest() *strategyv1.ChangeStrategyRiskRequest {
	return &strategyv1.ChangeStrategyRiskRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:      testSessionToken,
		IdempotencyKey: "risk-rpc-original-request-01",
		StrategyId:     9007199254740994,
		Input: &strategyv1.ChangeStrategyRiskInput{
			Action:           string(domain.AdminRiskBlock),
			ExpectedRevision: 9007199254740993,
			Reason:           "  explicit risk closure  ",
		},
	}
}

func TestRiskGRPCPreservesTypedAmountsProofAndIndependentVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &riskRPCStore{}
	reader, err := NewRiskQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewRiskControl(store)
	if err != nil {
		t.Fatal(err)
	}
	request := riskRPCRequest()
	state, err := reader.GetStrategyRisk(ctx, &strategyv1.GetStrategyRiskRequest{
		Session:    request.Session,
		StrategyId: request.StrategyId,
	})
	if err != nil || state.Revision != 9007199254740993 || state.LimitNotional != "300.000000000000000001" || state.ReservedNotional != "123.000000000000000001" || state.RequestedConfigVersion != 9007199254740995 || state.DesiredState != string(domain.StrategyStatePaused) || store.token != testSessionToken {
		t.Fatal("risk read lost precise fields or current proof", err)
	}
	receipt, err := writer.ChangeStrategyRisk(ctx, request)
	if err != nil || receipt.StrategyId != request.StrategyId || receipt.Revision != 9007199254740994 || receipt.Enabled || store.csrf != testSessionToken || store.request.Reason != "explicit risk closure" {
		t.Fatal("risk write lost proof, identity or normalized immutable receipt", err)
	}
	request.Input.Action = string(domain.AdminRiskUnblock)
	request.Input.ExpectedConfigVersion = state.RequestedConfigVersion
	request.Input.ExpectedDesiredState = state.DesiredState
	receipt, err = writer.ChangeStrategyRisk(ctx, request)
	if err != nil || !receipt.Enabled || receipt.ExpectedConfigVersion != state.RequestedConfigVersion || receipt.ExpectedDesiredState != state.DesiredState {
		t.Fatal("unblock lost its explicit confirmation binding", err)
	}
}

func TestRiskGRPCRejectsAuthorityUnknownFieldsAndAmbiguousConfirmation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &riskRPCStore{}
	writer, err := NewRiskControl(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ChangeStrategyRisk(verified(ctx, rpcsecurity.ServicePortfolio), riskRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API risk control accepted", err)
	}
	for _, mutate := range []func(*strategyv1.ChangeStrategyRiskRequest){
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input = nil },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.StrategyId = 0 },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input.ExpectedRevision = math.MaxInt64 },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input.Action = "" },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input.ExpectedConfigVersion = 1 },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input.Action = string(domain.AdminRiskUnblock) },
		func(r *strategyv1.ChangeStrategyRiskRequest) { r.Input.Reason = strings.Repeat("x", 3000) },
		func(r *strategyv1.ChangeStrategyRiskRequest) {
			r.Input.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
	} {
		request := proto.CloneOf(riskRPCRequest())
		mutate(request)
		if _, err := writer.ChangeStrategyRisk(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid risk control reached persistence", err)
		}
	}
	request := riskRPCRequest()
	request.CsrfToken = ""
	if _, err := writer.ChangeStrategyRisk(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.PermissionDenied || store.calls != 0 {
		t.Fatal("missing CSRF or invalid input reached persistence", err)
	}
}

func TestRiskGRPCMapsRecoveryAndPrivateFailuresWithoutPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &riskRPCStore{}
	writer, err := NewRiskControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []struct {
		err  error
		code codes.Code
	}{
		{
			err:  domain.ErrAdminRiskRecoveryDenied,
			code: codes.FailedPrecondition,
		},
		{
			err:  domain.ErrForbidden,
			code: codes.PermissionDenied,
		},
		{
			err:  domain.ErrUnauthenticated,
			code: codes.Unauthenticated,
		},
		{
			err:  domain.ErrAdminConflict,
			code: codes.Aborted,
		},
		{
			err:  domain.ErrAdminNotFound,
			code: codes.NotFound,
		},
		{
			err:  errors.New("private-risk-storage-details"),
			code: codes.Unavailable,
		},
	} {
		store.err = failure.err
		value, err := writer.ChangeStrategyRisk(ctx, riskRPCRequest())
		if value != nil || status.Code(err) != failure.code || strings.Contains(err.Error(), "private-risk-storage-details") {
			t.Fatal("risk failure leaked a partial success, cause or wrong status", err)
		}
	}
	store.err, store.badReceipt = nil, true
	if value, err := writer.ChangeStrategyRisk(ctx, riskRPCRequest()); value != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("mismatched successful receipt escaped", err)
	}
	reader, err := NewRiskQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	store.badState = true
	if value, err := reader.GetStrategyRisk(ctx, &strategyv1.GetStrategyRiskRequest{
		Session:    riskRPCRequest().Session,
		StrategyId: riskRPCRequest().StrategyId,
	}); value != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("mismatched successful state escaped", err)
	}
}
