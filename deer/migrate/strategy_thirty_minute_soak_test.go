//go:build integration && soak

package migrate

import (
	"testing"
	"time"
)

func TestStrategyThirtyMinuteRealTimeSoak(t *testing.T) {
	runStrategySoak(t, 30*time.Minute, 600)
}
