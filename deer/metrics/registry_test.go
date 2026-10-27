package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"google.golang.org/grpc/codes"
)

func TestMetricsRemainExactAndPrometheusCompatible(t *testing.T) {
	t.Parallel()
	registry := New()
	registry.ObserveHTTP(200, time.Nanosecond)
	registry.ObserveHTTP(503, 2*time.Nanosecond)
	registry.ObserveGRPC(codes.Unavailable, 10*time.Millisecond)
	output := string(registry.Render())
	for _, sample := range []string{
		`deer_requests_total{transport="http",code="2xx"} 1`,
		`deer_requests_total{transport="http",code="5xx"} 1`,
		`deer_request_errors_total{transport="http"} 1`,
		`deer_request_duration_seconds_sum{transport="http"} 0.000000003`,
		`deer_request_duration_seconds_bucket{transport="http",le="+Inf"} 2`,
		`deer_request_duration_seconds_count{transport="http"} 2`,
	} {
		if !strings.Contains(output, sample+"\n") {
			t.Fatalf("missing exact metric %s", sample)
		}
	}
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(output))
	if err != nil || len(families) != 3 {
		t.Fatalf("Prometheus parser = %d families, %v", len(families), err)
	}
	other := string(New().Render())
	if !strings.Contains(other, `deer_request_duration_seconds_count{transport="http"} 0`) {
		t.Fatal("metric state leaked into another instance")
	}
}

func TestMetricsBoundLabelsAndJoinConcurrentUpdates(t *testing.T) {
	t.Parallel()
	registry := New()
	var work sync.WaitGroup
	for range 8 {
		work.Go(func() {
			for range 100 {
				registry.ObserveHTTP(9876, time.Millisecond)
				registry.ObserveGRPC(codes.Code(9876), time.Millisecond)
			}
		})
	}
	work.Go(func() {
		for range 20 {
			var parser expfmt.TextParser
			families, err := parser.TextToMetricFamilies(strings.NewReader(string(registry.Render())))
			if err != nil {
				t.Error(err)
				return
			}
			for _, metric := range families["deer_request_duration_seconds"].GetMetric() {
				histogram := metric.GetHistogram()
				buckets := histogram.GetBucket()
				if len(buckets) == 0 || buckets[len(buckets)-1].GetCumulativeCount() != histogram.GetSampleCount() {
					t.Error("concurrent scrape has inconsistent count and infinite bucket")
					return
				}
			}
		}
	})
	work.Wait()
	output := string(registry.Render())
	if strings.Contains(output, "9876") || !strings.Contains(output, `deer_requests_total{transport="http",code="other"} 800`) || !strings.Contains(output, `deer_requests_total{transport="grpc",code="Unknown"} 800`) {
		t.Fatal("concurrent observations lost counts or created unbounded labels")
	}
}
