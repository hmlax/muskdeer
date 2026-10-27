package marketgrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketview"
)

type clientFunc func(context.Context, *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error)

func (f clientFunc) GetReference(ctx context.Context, request *marketv1.GetReferenceRequest, _ ...grpc.CallOption) (*marketv1.GetReferenceReply, error) {
	return f(ctx, request)
}

func TestReaderRejectsOtherSourceModeAndMalformedRepliesWithoutFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	view, err := marketview.ToProto(storeFixture().value.MarketReference)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*marketv1.MarketReference){
		func(value *marketv1.MarketReference) { value.Source = string(domain.MarketSourceOKX) },
		func(value *marketv1.MarketReference) { value.Mode = string(domain.MarketModeLive) },
		func(value *marketv1.MarketReference) { value.InstrumentId++ },
		func(value *marketv1.MarketReference) { value.BidPrice = "1e2" },
	} {
		calls := 0
		client := &reader{client: clientFunc(func(_ context.Context, request *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error) {
			calls++
			if request.Source != string(domain.MarketSourceBinance) || request.Mode != string(domain.MarketModeReplay) {
				t.Fatal("request changed explicit source or mode")
			}
			candidate := proto.Clone(view).(*marketv1.MarketReference)
			mutate(candidate)
			return &marketv1.GetReferenceReply{Reference: candidate}, nil
		})}
		if _, err := client.Reference(ctx, domain.MarketSourceBinance, 1, domain.MarketModeReplay); err == nil || calls != 1 {
			t.Fatal("malformed observation succeeded or retried")
		}
	}
	failure := errors.New("lookup failed")
	calls := 0
	client := &reader{client: clientFunc(func(context.Context, *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error) {
		calls++
		return nil, failure
	})}
	if _, err := client.Reference(ctx, domain.MarketSourceBinance, 1, domain.MarketModeReplay); !errors.Is(err, failure) || calls != 1 {
		t.Fatal("failed source was retried or substituted")
	}
}

func TestReaderRechecksExpiryAfterTransportAndCannotConsumeBaseline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	value := storeFixture().value.MarketReference
	value.Reading.EventTime, value.Reading.ReceivedAt, value.Reading.EvaluatedAt = now.Add(-2*time.Second), now.Add(-2*time.Second), now.Add(-2*time.Second)
	value.Reading.ValidUntil = now.Add(-time.Second)
	view, err := marketview.ToProto(value)
	if err != nil {
		t.Fatal(err)
	}
	client := &reader{client: clientFunc(func(context.Context, *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error) {
		return &marketv1.GetReferenceReply{Reference: view}, nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reading, err := client.Reference(ctx, domain.MarketSourceBinance, 1, domain.MarketModeReplay)
	if err != nil || reading.Reading.Quality != domain.MarketQualityStale || !reading.Reading.Mid.IsZero() || !reading.Reading.ValidUntil.IsZero() {
		t.Fatalf("RPC transport extended reference expiry: %+v, %v", reading, err)
	}
	value.AuthorityUntil = now.Add(-time.Second)
	if current := currentReference(value, now); current.Reading.Quality != domain.MarketQualityUnavailable || !current.Reading.Mid.IsZero() {
		t.Fatal("expired publisher retained reference on client")
	}
}
