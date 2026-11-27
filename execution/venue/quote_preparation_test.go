package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestMakerPreparationEndsWithInitialObservation(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failed], func(t *testing.T) {
			runtime, client, profile := makerObservationRuntime()
			runtime.cfg.OperationTimeout = time.Second
			runtime.quoteTurns = newQuoteSendBudget(1)
			if failed {
				runtime.client.Store(&gatewaySession{Client: &failedPreparationClient{failure: context.DeadlineExceeded}})
			}
			work, release, err := runtime.admitMakerTurn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			_, _, err = runtime.read(work, profile)
			if failed != errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("observation error=%v", err)
			}
			if runtime.quoteTurns.active != 0 {
				t.Fatal("completed observation retained preparation capacity for target calculations or protection")
			}
			if work.Err() != nil || !failed && client.reads.Load() != 1 {
				t.Fatal("releasing preparation canceled the writer or repeated its observation")
			}
			second, err := runtime.quoteTurns.acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			release() // The original turn's cleanup must not release the next fund.
			if runtime.quoteTurns.active != 1 {
				t.Fatal("turn cleanup released another fund's preparation")
			}
			second.release()
		})
	}
}

type failedPreparationClient struct {
	gateway.Client
	failure error
}

func (c *failedPreparationClient) Read(context.Context, gateway.Scope) (*gateway.Observation, error) {
	return nil, c.failure
}

func TestSerialSDKAdmissionDoesNotHoldPreparation(t *testing.T) {
	runtime, _, _ := makerObservationRuntime()
	runtime.cfg.OperationTimeout = time.Second
	runtime.quoteTurns = newQuoteSendBudget(1)
	runtime.quoteBudget = newQuoteSendBudget(1)
	work, release, err := runtime.admitMakerTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, finish, err := runtime.reserveQuoteSend(work)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if runtime.quoteTurns.active != 0 || runtime.quoteBudget.active != 1 {
		t.Fatal("serial cancellation retained observation preparation while owning an SDK slot")
	}
}
