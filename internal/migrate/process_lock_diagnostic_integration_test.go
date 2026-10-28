//go:build integration

package migrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

type processLockDiagnostic struct {
	PID         int `gorm:"column:pid"`
	Application string
	WaitType    string
	WaitEvent   string
	Blockers    string
	Query       string
}

// traceProcessLocks retains at most ten samples for this isolated database's
// explicitly named test processes. It emits them only when the scenario fails.
func traceProcessLocks(ctx context.Context, t *testing.T, db *gorm.DB, applications []string) {
	t.Helper()
	run, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	var samples [][]processLockDiagnostic
	var firstStall []processLockDiagnostic
	go func() {
		defer close(finished)
		ticks := time.NewTicker(50 * time.Millisecond)
		defer ticks.Stop()
		waits := make(map[int]time.Time)
		for {
			select {
			case <-run.Done():
				return
			case <-ticks.C:
			}
			call, stop := context.WithTimeout(run, 50*time.Millisecond)
			var rows []processLockDiagnostic
			// Read-only system diagnostics, like waitBackendLock. The generated
			// ledger binds this fixture's schema without string table selection.
			err := db.WithContext(call).Model(&modelv1.SchemaMigration{}).
				Select("pg_stat_activity.pid AS pid, pg_stat_activity.application_name AS application, COALESCE(pg_stat_activity.wait_event_type, '') AS wait_type, COALESCE(pg_stat_activity.wait_event, '') AS wait_event, pg_blocking_pids(pg_stat_activity.pid)::text AS blockers, pg_stat_activity.query AS query").
				Joins("JOIN pg_stat_activity ON pg_stat_activity.datname = current_database()").
				Where("version = ? AND pg_stat_activity.application_name IN ? AND pg_stat_activity.state <> ?", 1, applications, "idle").
				Limit(64).Find(&rows).Error
			stop()
			if err != nil {
				continue
			}
			waiting := false
			current := make(map[int]time.Time)
			for _, row := range rows {
				if row.WaitType != "Lock" {
					continue
				}
				waiting = true
				started, exists := waits[row.PID]
				if !exists {
					started = time.Now()
				}
				current[row.PID] = started
				if firstStall == nil && time.Since(started) >= 500*time.Millisecond {
					firstStall = rows
				}
			}
			waits = current
			if waiting {
				samples = append(samples, rows)
				if len(samples) > 10 {
					samples = samples[1:]
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
		if !t.Failed() {
			return
		}
		for _, row := range firstStall {
			t.Logf("first sustained lock pid=%d application=%s wait=%s/%s blockers=%s query=%s", row.PID, row.Application, row.WaitType, row.WaitEvent, row.Blockers, strings.Join(strings.Fields(row.Query), " "))
		}
		for index, sample := range samples {
			for _, row := range sample {
				t.Logf("lock sample=%d pid=%d application=%s wait=%s/%s blockers=%s query=%s", index, row.PID, row.Application, row.WaitType, row.WaitEvent, row.Blockers, strings.Join(strings.Fields(row.Query), " "))
			}
		}
	})
}
