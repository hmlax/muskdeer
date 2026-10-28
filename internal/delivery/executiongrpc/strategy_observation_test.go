package executiongrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyObservationClassifiesMissingAndUncertainWithoutWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	wire := &strategyConnectionProbe{failure: status.Error(codes.NotFound, "private missing detail")}
	route := &strategyRouteProbe{}
	client, err := newStrategyClient(&strategyFactoryProbe{connection: wire}, route)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Order(ctx, "account", "original-client"); !errors.Is(err, domain.ErrCoreNotFound) {
		t.Fatalf("missing observation changed category: %v", err)
	}
	wire.failure = status.Error(codes.Unavailable, "private transport detail")
	if _, err := client.FindCancellation(ctx, "account", "original-cancel-key"); !errors.Is(err, domain.ErrOutcomeUnknown) || err.Error() != domain.MessageOutcomeUnknown {
		t.Fatalf("unavailable observation was interpreted as absence or leaked detail: %v", err)
	}
	if route.calls != 0 || wire.calls != 2 || wire.submit != nil || wire.cancel != nil {
		t.Fatal("observation routed to a writer or sent a command")
	}
}
