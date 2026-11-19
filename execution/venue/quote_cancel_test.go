package venue

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

type heldCancelClient struct {
	gateway.Client
	started chan uint64
	release chan struct{}
	calls   atomic.Int64
	active  atomic.Int64
	maximum atomic.Int64
	failure error
}

func (c *heldCancelClient) SubmitQuote(context.Context, gateway.QuoteRequest) (*gateway.SubmitResult, error) {
	return nil, errors.New("cancellation must never place orders")
}

func (c *heldCancelClient) CancelQuote(ctx context.Context, request gateway.CancelRequest) error {
	c.calls.Add(1)
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for previous := c.maximum.Load(); active > previous; previous = c.maximum.Load() {
		if c.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	c.started <- request.OrderID
	if request.OrderID == 1 && c.failure != nil {
		return c.failure
	}
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMakerCancellationsAreBoundedAndJoinWithoutReplay(t *testing.T) {
	for _, kind := range []string{"success", "partial_failure", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			client := &heldCancelClient{
				started: make(chan uint64, 8),
				release: make(chan struct{}),
			}
			if kind == "partial_failure" {
				client.failure = errors.New("ambiguous cancellation")
			}
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: 1,
					Template:   domain.StrategyTemplateMarketMaker,
				},
				client:     &atomic.Pointer[gatewaySession]{},
				quoteSlots: make(chan struct{}, maximumConcurrentQuoteSends),
			}
			runtime.client.Store(&gatewaySession{Client: client})
			var orders []*common.VenueOrder
			for id := 1; id <= 8; id++ {
				orders = append(orders, &common.VenueOrder{OrderId: strconv.Itoa(id)})
			}
			result := &pb.QuoteResult{}
			done := make(chan error, 1)
			go func() {
				done <- runtime.cancelOrders(ctx, domain.LeaseToken{}, &common.VenueProfilePolicy{}, orders, result)
			}()
			seen := make(map[uint64]bool)
			timer := time.NewTimer(500 * time.Millisecond)
			defer timer.Stop()
			for range 4 {
				select {
				case id := <-client.started:
					seen[id] = true
				case <-timer.C:
					cancel()
					err := <-done
					t.Fatalf("only %d cancellations started before an acknowledgement (joined: %v)", len(seen), err)
				}
			}
			select {
			case err := <-done:
				t.Fatalf("writer returned with in-flight cancellations: %v", err)
			default:
			}
			if kind == "cancelled" {
				cancel()
			} else {
				close(client.release)
			}
			err := <-done
			wantCalls, wantSuccess := int64(8), uint32(8)
			switch kind {
			case "partial_failure":
				wantCalls, wantSuccess = 4, 3
				if !errors.Is(err, client.failure) {
					t.Fatal(err)
				}
			case "cancelled":
				wantCalls, wantSuccess = 4, 0
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			for len(client.started) > 0 {
				id := <-client.started
				if seen[id] {
					t.Fatal("cancellation was replayed")
				}
				seen[id] = true
			}
			if client.calls.Load() != wantCalls || result.Cancelled != wantSuccess || client.active.Load() != 0 || client.maximum.Load() > maximumQuoteWave || len(runtime.quoteSlots) != 0 {
				t.Fatalf("calls=%d success=%d active=%d slots=%d", client.calls.Load(), result.Cancelled, client.active.Load(), len(runtime.quoteSlots))
			}
		})
	}
}

func TestMakerCancellationWaitConsumesSharedTransportDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	client := &heldCancelClient{
		started: make(chan uint64, 1),
		release: make(chan struct{}),
	}
	runtime := &Runtime{
		cfg: &venueconfig.RuntimeConfig{
			InstanceID: 1,
			Template:   domain.StrategyTemplateMarketMaker,
		},
		client:     &atomic.Pointer[gatewaySession]{},
		quoteSlots: make(chan struct{}, 1),
	}
	runtime.client.Store(&gatewaySession{Client: client})
	runtime.quoteSlots <- struct{}{}
	err := runtime.sendQuoteCancellation(ctx, domain.LeaseToken{}, &common.VenueProfilePolicy{}, &common.VenueOrder{OrderId: "1"})
	if !errors.Is(err, context.DeadlineExceeded) || client.calls.Load() != 0 || len(runtime.quoteSlots) != 1 {
		t.Fatalf("wait error=%v calls=%d slots=%d", err, client.calls.Load(), len(runtime.quoteSlots))
	}
}
