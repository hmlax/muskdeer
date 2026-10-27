package deer

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	healthAlive          = "alive"
	healthReady          = "ready"
	healthUnready        = "unready"
	healthStopped        = "stopped"
	maximumHealthWatches = 4
	healthPollInterval   = 250 * time.Millisecond
)

type healthResponse struct {
	Status string `json:"status"`
}

type readinessHealth struct {
	healthv1.UnimplementedHealthServer
	owner      *Deer
	watchSlots chan struct{}
}

// MonitoringAddress returns the reserved local probe address, or empty if disabled.
func (d *Deer) MonitoringAddress() string { return d.monitoringAddress }

// Live reports whether Run still owns the process, including startup and drain.
// Dependency failures affect CheckReady, rather than declaring this process dead.
func (d *Deer) Live() bool {
	d.mu.Lock()
	started := d.started
	d.mu.Unlock()
	if !started {
		return false
	}
	select {
	case <-d.done:
		return false
	default:
		return true
	}
}

func (d *Deer) addMonitoring(ctx context.Context) error {
	hertz, address, err := d.addHertz(ctx, d.cfg.Monitoring, "monitoring HTTP")
	if err != nil {
		return err
	}
	d.monitoringAddress = address
	hertz.GET("/livez", func(_ context.Context, response *app.RequestContext) {
		if !d.Live() {
			response.JSON(http.StatusServiceUnavailable, healthResponse{Status: healthStopped})
			return
		}
		response.JSON(http.StatusOK, healthResponse{Status: healthAlive})
	})
	hertz.GET("/readyz", func(ctx context.Context, response *app.RequestContext) {
		if err := d.CheckReady(ctx); err != nil {
			response.JSON(http.StatusServiceUnavailable, healthResponse{Status: healthUnready})
			return
		}
		response.JSON(http.StatusOK, healthResponse{Status: healthReady})
	})
	return nil
}

func (s *readinessHealth) Check(ctx context.Context, request *healthv1.HealthCheckRequest) (*healthv1.HealthCheckResponse, error) {
	if !s.knownService(request.GetService()) {
		return nil, status.Error(codes.NotFound, "health service is unknown")
	}
	return &healthv1.HealthCheckResponse{Status: s.servingStatus(ctx)}, nil
}

func (s *readinessHealth) List(ctx context.Context, _ *healthv1.HealthListRequest) (*healthv1.HealthListResponse, error) {
	current := s.servingStatus(ctx)
	return &healthv1.HealthListResponse{
		Statuses: map[string]*healthv1.HealthCheckResponse{
			"":                                   {Status: current},
			s.owner.cfg.Name + grpcServiceSuffix: {Status: current},
		},
	}, nil
}

func (s *readinessHealth) Watch(request *healthv1.HealthCheckRequest, stream healthv1.Health_WatchServer) error {
	select {
	case s.watchSlots <- struct{}{}:
		defer func() { <-s.watchSlots }()
	default:
		return status.Error(codes.ResourceExhausted, "health watch capacity exhausted")
	}
	ctx := stream.Context()
	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()
	previous := healthv1.HealthCheckResponse_UNKNOWN
	for {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		current := healthv1.HealthCheckResponse_SERVICE_UNKNOWN
		if s.knownService(request.GetService()) {
			current = s.servingStatus(ctx)
		}
		if current != previous {
			if err := stream.Send(&healthv1.HealthCheckResponse{Status: current}); err != nil {
				return err
			}
			previous = current
		}
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-ticker.C:
		}
	}
}

func (s *readinessHealth) knownService(service string) bool {
	return service == "" || service == s.owner.cfg.Name+grpcServiceSuffix
}

func (s *readinessHealth) servingStatus(ctx context.Context) healthv1.HealthCheckResponse_ServingStatus {
	if err := s.owner.CheckReady(ctx); err != nil {
		return healthv1.HealthCheckResponse_NOT_SERVING
	}
	return healthv1.HealthCheckResponse_SERVING
}
