// Package metrics owns fixed-cardinality Prometheus counters and histograms.
// Durations remain decimal values; no floating-point conversion is used.
package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
)

const (
	httpTransport = iota
	grpcTransport
	probeTransport
	transportCount
	latencyBucketCount = 11
)

type statistics struct {
	byCode  [17]uint64
	count   uint64
	errors  uint64
	buckets [latencyBucketCount]uint64
	sum     decimal.Decimal
}

// Registry is one instance's in-memory telemetry. It has no global registration,
// export queue, goroutine or network connection. Render snapshots bounded state.
type Registry struct {
	mu      sync.Mutex
	traffic [transportCount]statistics
}

// New creates an isolated registry with a fixed label/bucket domain.
func New() *Registry { return &Registry{} }

// ObserveHTTP records an API request by status class, without raw route/ID labels.
func (r *Registry) ObserveHTTP(status int, elapsed time.Duration) {
	class := httpClass(status)
	r.observe(httpTransport, class, class == 0 || class >= 4, elapsed)
}

// ObserveProbe separates local probe latency from business HTTP latency.
func (r *Registry) ObserveProbe(status int, elapsed time.Duration) {
	class := httpClass(status)
	r.observe(probeTransport, class, class == 0 || class >= 4, elapsed)
}

// ObserveGRPC records a server RPC; unknown status values collapse to Unknown.
func (r *Registry) ObserveGRPC(code codes.Code, elapsed time.Duration) {
	if code > codes.Unauthenticated {
		code = codes.Unknown
	}
	r.observe(grpcTransport, int(code), code != codes.OK, elapsed)
}

func (r *Registry) observe(transport, code int, failed bool, elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := &r.traffic[transport]
	entry.count++
	entry.byCode[code]++
	if failed {
		entry.errors++
	}
	entry.sum = entry.sum.Add(decimal.NewFromInt(elapsed.Nanoseconds()).Shift(-9))
	for index, upper := range latencyBounds() {
		if elapsed <= upper {
			entry.buckets[index]++
		}
	}
}

// Render returns Prometheus text 0.0.4 without holding a lock during encoding.
// State has fixed size; decimal sums are immutable values after each Add.
func (r *Registry) Render() []byte {
	r.mu.Lock()
	snapshot := r.traffic
	r.mu.Unlock()
	output := make([]byte, 0, 16*1024)
	transports := [...]string{"http", "grpc", "probe"}
	classes := [...]string{"other", "1xx", "2xx", "3xx", "4xx", "5xx"}
	// sum(rate(deer_requests_total[5m])) by (transport, code)
	output = header(output, "deer_requests_total", "Total completed requests by transport and bounded status class.", "counter")
	for transport, name := range transports {
		for code := range snapshot[transport].byCode {
			var label string
			if transport == grpcTransport {
				label = codes.Code(code).String()
			} else {
				if code >= len(classes) {
					break
				}
				label = classes[code]
			}
			output = sample(output, "deer_requests_total", `transport="`+name+`",code="`+label+`"`, strconv.FormatUint(snapshot[transport].byCode[code], 10))
		}
	}
	// sum(rate(deer_request_errors_total[5m])) by (transport)
	output = header(output, "deer_request_errors_total", "Total unsuccessful requests by transport.", "counter")
	for transport, name := range transports {
		output = sample(output, "deer_request_errors_total", `transport="`+name+`"`, strconv.FormatUint(snapshot[transport].errors, 10))
	}
	// histogram_quantile(0.99, sum(rate(deer_request_duration_seconds_bucket[5m])) by (le, transport))
	output = header(output, "deer_request_duration_seconds", "Request duration in seconds by transport.", "histogram")
	for transport, name := range transports {
		labels := `transport="` + name + `"`
		for index, upper := range latencyBounds() {
			bound := decimal.NewFromInt(upper.Nanoseconds()).Shift(-9).String()
			output = sample(output, "deer_request_duration_seconds_bucket", labels+`,le="`+bound+`"`, strconv.FormatUint(snapshot[transport].buckets[index], 10))
		}
		count := strconv.FormatUint(snapshot[transport].count, 10)
		output = sample(output, "deer_request_duration_seconds_bucket", labels+`,le="+Inf"`, count)
		output = sample(output, "deer_request_duration_seconds_sum", labels, snapshot[transport].sum.String())
		output = sample(output, "deer_request_duration_seconds_count", labels, count)
	}
	return output
}

func latencyBounds() [latencyBucketCount]time.Duration {
	return [latencyBucketCount]time.Duration{
		time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
		25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
		250 * time.Millisecond, 500 * time.Millisecond, time.Second,
		5 * time.Second, 10 * time.Second,
	}
}

func httpClass(status int) int {
	if status < 100 || status >= 600 {
		return 0
	}
	return status / 100
}

func header(output []byte, name, help, kind string) []byte {
	output = append(output, "# HELP "...)
	output = append(output, name...)
	output = append(output, ' ')
	output = append(output, help...)
	output = append(output, "\n# TYPE "...)
	output = append(output, name...)
	output = append(output, ' ')
	output = append(output, kind...)
	return append(output, '\n')
}

func sample(output []byte, name, labels, value string) []byte {
	output = append(output, name...)
	output = append(output, '{')
	output = append(output, labels...)
	output = append(output, "} "...)
	output = append(output, value...)
	return append(output, '\n')
}
