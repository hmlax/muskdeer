package executiongrpc

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyPagesRejectCrossScopeDuplicateAndBrokenCursors(t *testing.T) {
	input := domain.OrderInput{
		IdempotencyKey: "first",
		AccountID:      "account",
		InstrumentID:   1,
		Authority: domain.StrategyAuthority{
			StrategyID:    1,
			ConfigVersion: 1,
		},
		Price:    decimal.NewFromInt(99),
		Quantity: decimal.NewFromInt(1),
	}
	first := strategyReplyFixture(input).Order
	input.IdempotencyKey = "second"
	second := strategyReplyFixture(input).Order
	second.IntentId = 2
	page := &executionv1.ListOrdersReply{
		Orders:      []*executionv1.OrderView{first, second},
		NextAfterId: 2,
	}
	filter := domain.ExecutionFilter{
		AccountID:    "account",
		StrategyID:   1,
		InstrumentID: 1,
		Limit:        2,
		OpenOnly:     true,
	}
	orders, next, err := strategyOrderPage(page, filter)
	if err != nil || len(orders) != 2 || next != 2 {
		t.Fatalf("valid complete page changed: %+v, %d, %v", orders, next, err)
	}
	for _, mutate := range []func(*executionv1.ListOrdersReply){
		func(v *executionv1.ListOrdersReply) { v.NextAfterId = 3 },
		func(v *executionv1.ListOrdersReply) { v.Orders[1].IntentId = 1 },
		func(v *executionv1.ListOrdersReply) { v.Orders[0].AccountId = "another-account" },
		func(v *executionv1.ListOrdersReply) { v.Orders[0].StrategyInstanceId = 2 },
		func(v *executionv1.ListOrdersReply) { v.Orders[0].InstrumentId = 2 },
		func(v *executionv1.ListOrdersReply) { v.Orders = v.Orders[:1] },
		func(v *executionv1.ListOrdersReply) { v.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	} {
		changed := proto.CloneOf(page)
		mutate(changed)
		if _, _, err := strategyOrderPage(changed, filter); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatal("partial, duplicate or wrong-scope observation accepted")
		}
	}
}

func TestStrategyAccountPreservesLedgerPrecisionAndRejectsAmbiguousAssets(t *testing.T) {
	exact := "99999999999999999999.123456789012345678901234567890123456"
	reply := &executionv1.AccountReply{
		AccountId:  "account",
		Sequence:   1,
		ObservedAt: timestamppb.Now(),
		Balances: []*executionv1.AssetBalance{{
			Asset:     "USDT",
			Available: exact,
			Frozen:    "0",
		}},
	}
	account, err := strategyAccountObservation(reply, "account")
	if err != nil || len(account.Balances) != 1 || account.Balances[0].Available.String() != exact || !account.Balances[0].Frozen.IsZero() {
		t.Fatalf("ledger observation lost exact decimals: %+v, %v", account, err)
	}
	for _, mutate := range []func(*executionv1.AccountReply){
		func(v *executionv1.AccountReply) { v.AccountId = "another-account" },
		func(v *executionv1.AccountReply) { v.ObservedAt = nil },
		func(v *executionv1.AccountReply) { v.Balances = append(v.Balances, proto.CloneOf(v.Balances[0])) },
		func(v *executionv1.AccountReply) { v.Balances[0].Available = "1e3" },
		func(v *executionv1.AccountReply) { v.Balances[0].Frozen = "-1" },
		func(v *executionv1.AccountReply) { v.Balances[0].ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	} {
		changed := proto.CloneOf(reply)
		mutate(changed)
		if _, err := strategyAccountObservation(changed, "account"); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatal("ambiguous asset or invalid ledger observation accepted")
		}
	}
}
