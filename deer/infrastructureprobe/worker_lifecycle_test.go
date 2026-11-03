//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestWorkerFailureWithdrawsRegisteredOwner(t *testing.T) {
	const serviceName = "worker-lifecycle-probe"
	failure := errors.New("controlled registered worker failure")
	gate := make(chan struct{})
	cfg := serviceConfig(t, "worker-owner")
	cfg.Name = serviceName
	cfg.Postgres = nil
	cfg.Nacos = namingConfig()
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(time.Second),
	}
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		Workers: func(context.Context, *deer.Deer) ([]deer.Worker, error) {
			return []deer.Worker{
				{
					Name: "failure-probe",
					Run: func(ctx context.Context) error {
						select {
						case <-gate:
							return failure
						case <-ctx.Done():
							return ctx.Err()
						}
					},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runNamingService(t, service, cleanup)
	observer := namingObserver(t)
	waitNamingCount(t, observer, serviceName+".grpc", 1)
	close(gate)
	select {
	case err := <-result:
		if !errors.Is(err, failure) || service.Ready() {
			t.Fatalf("worker failure lost ownership termination: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker failure did not terminate its registered owner")
	}
	waitNamingCount(t, observer, serviceName+".grpc", 0)
}
