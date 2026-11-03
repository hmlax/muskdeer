package orderclient_test

import (
	"encoding/hex"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"testing"

	"gitlab.com/himalaya/lukla-go"
	"gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorCodeFromMessage(t *testing.T) {
	st := status.New(codes.InvalidArgument, "[E1003] symbol required")
	err := st.Err()
	if got := orderclient.ErrorCodeFrom(err); got != 1003 {
		t.Fatalf("ErrorCodeFrom() = %d, want 1003", got)
	}
	re, ok := orderclient.AsRPCError(err)
	if !ok || re.ErrorCode != 1003 {
		t.Fatalf("AsRPCError: %+v, ok=%v", re, ok)
	}
}

func TestOpenPositionInputProto(t *testing.T) {
	in := orderclient.OpenPositionInput{
		UserID:    1,
		Symbol:    "BTCUSDT",
		Side:      pb.Side_SIDE_BUY,
		OrderKind: pb.OrderKind_ORDER_KIND_LIMIT,
		Price:     "100000",
		Quantity:  "0.01",
		Leverage:  10,
		ClientID:  "cid-1",
	}
	req := in.Proto()
	if req.ClientId != "cid-1" || req.Symbol != "BTCUSDT" {
		t.Fatalf("proto: %+v", req)
	}
}

// Golden bytes protect canonical field numbers and enum values from stale SDK regeneration.
func TestCanonicalOrderWireContract(t *testing.T) {
	request := &pb.OpenPositionRequest{
		UserId: 1,
		FundId: 2,
		Symbol: "X",
		Side:   pb.Side_SIDE_BUY,
	}
	bytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(bytes); got != "080110021a01582001" {
		t.Fatalf("wire contract changed: %s", got)
	}
	fields := request.ProtoReflect().Descriptor().Fields()
	for name, tag := range map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "fund_id": 2, "symbol": 3, "side": 4,
		"price": 6, "quantity": 7, "position_id": 13, "client_id": 16,
		"maker_fee_rate_ppm": 18, "taker_fee_rate_ppm": 19,
	} {
		if fields.ByName(name).Number() != tag {
			t.Fatalf("tag changed: %s", name)
		}
	}
	if pb.Side_SIDE_BUY != 1 || pb.Side_SIDE_SELL != 2 {
		t.Fatal("side enum contract changed")
	}
}
