package deer

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
)

func TestWorkerCapacityOwnsAndJoinsNativeSymbolLanes(t *testing.T) {
	for _, count := range []int{34, 65, 257} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			var stopped, released atomic.Int64
			service, cleanup, err := NewDeer(t.Context(), serviceConfig("worker-capacity"), ServiceSpec{
				LogOutput: io.Discard,
				Resources: []Initializer{countedResource(&released)},
				Workers: func(context.Context, *Deer) ([]Worker, error) {
					workers := make([]Worker, 0, count)
					for index := range count {
						workers = append(workers, Worker{
							Name: fmt.Sprintf("symbol-lane-%d", index),
							Run: func(ctx context.Context) error {
								<-ctx.Done()
								stopped.Add(1)
								return ctx.Err()
							},
						})
					}
					return workers, nil
				},
			})
			if err != nil {
				t.Fatalf("NewDeer(%d workers) = %v, want success", count, err)
			}
			result := runDeer(t, service)
			if err := cleanup(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			if stopped.Load() != int64(count) || released.Load() != 1 {
				t.Errorf("Close(%d workers) = stopped %d, released %d; want stopped %d, released 1", count, stopped.Load(), released.Load(), count)
			}
		})
	}
}
