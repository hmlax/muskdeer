//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

type soakResult struct {
	RecordKind string `json:"record_kind"`
	TargetMS   int64  `json:"target_ms"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	FillCount  int64  `json:"fill_count"`
	Passed     bool   `json:"passed"`
}

func TestStrategySoakDriverSmoke(t *testing.T) {
	runStrategySoak(t, 12*time.Second, 2)
}

func runStrategySoak(t *testing.T, duration time.Duration, minimumFills int64) {
	t.Helper()
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), duration+30*time.Second)
	defer cancel()
	directory := soakArtifactDirectory(t)
	progress, err := os.OpenFile(filepath.Join(directory, "progress.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	result := soakResult{
		RecordKind: soakRecordResult,
		TargetMS:   duration.Milliseconds(),
	}
	// Registered before actors: their cleanup and exit status precede acceptance.
	t.Cleanup(func() {
		if err := progress.Close(); err != nil {
			t.Error(err)
		}
		result.Passed = !t.Failed()
		data, err := json.Marshal(result)
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(directory, "result.json"), data, 0600); err != nil {
			t.Error(err)
		}
	})
	_, submit := admissionFixture(ctx, t, db)
	traceProcessLocks(ctx, t, db, []string{
		"migration-test",
		"market/business-market-soak",
		"execution/business-execution",
		"portfolio/business-portfolio",
		"strategy/business-strategy.soak",
	})
	core, _ := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	startStrategySoakProcesses(ctx, t, db, postgres, submit, duration+time.Minute, directory)
	waitStrategyProcessQuotes(ctx, t, db, submit.Input.Authority.StrategyID, 0)
	started := time.Now()
	nextSample, lastQuote, lastProjection := time.Duration(0), time.Duration(0), time.Duration(0)
	observe := func(elapsed time.Duration, fills int64) {
		result.ElapsedMS, result.FillCount = elapsed.Milliseconds(), fills
		call, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		bilateral, err := observeSoakQuotes(call, db, submit)
		if err != nil {
			t.Fatalf("soak quote observation at %s: %v", elapsed, err)
		}
		if bilateral {
			lastQuote = elapsed
		}
		if elapsed < nextSample {
			return
		}
		nextSample = elapsed + 2*time.Second
		sample, err := readSoakSample(call, db, submit)
		sample.RecordKind, sample.ElapsedMS = soakRecordSample, elapsed.Milliseconds()
		if err != nil {
			sample.InvariantFailed = true
			if writeErr := json.NewEncoder(progress).Encode(sample); writeErr != nil {
				t.Error(writeErr)
			}
			t.Fatalf("soak invariant at %s: %v", elapsed, err)
		}
		if sample.FillCount != fills {
			t.Fatal("driver fills differ from immutable core facts")
		}
		if sample.Reconciled {
			lastProjection = elapsed
		}
		sample.QuoteGapMS, sample.ProjectionGapMS = (elapsed - lastQuote).Milliseconds(), (elapsed - lastProjection).Milliseconds()
		if err := json.NewEncoder(progress).Encode(sample); err != nil {
			t.Fatal(err)
		}
		if elapsed-lastQuote > 10*time.Second || elapsed-lastProjection > 10*time.Second {
			t.Fatalf("soak exceeded its 10-second observation bound: elapsed_ms=%d quote_gap_ms=%d projection_gap_ms=%d", sample.ElapsedMS, sample.QuoteGapMS, sample.ProjectionGapMS)
		}
	}
	result.FillCount = driveStrategyFillsObserved(ctx, t, db, core, submit, duration, observe)
	result.ElapsedMS = time.Since(started).Milliseconds()
	if result.ElapsedMS < result.TargetMS || result.FillCount < minimumFills {
		t.Fatal("soak did not reach its physical duration and minimum actual fills")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	waitStrategyProcessesStopped(ctx, t, db, []int64{submit.Input.Authority.StrategyID})
	snapshot := waitStrategyFillProjection(ctx, t, db, portfolio, submit.Input.AccountID, result.FillCount)
	assertStrategyFillAccounting(ctx, t, db, core, portfolio, submit, snapshot, result.FillCount)
	t.Logf("physical soak elapsed_ms=%d fills=%d artifacts=%s", result.ElapsedMS, result.FillCount, directory)
}

func soakArtifactDirectory(t *testing.T) string {
	t.Helper()
	path := os.Getenv("MUSKDEER_SOAK_ARTIFACTS")
	if path == "" {
		return t.TempDir()
	}
	root, err := filepath.EvalSymlinks(filepath.Join("..", "..", ".cache", "soak"))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		t.Fatal("soak artifacts must be in a dedicated .cache/soak child directory")
	}
	return resolved
}

func startStrategySoakProcesses(ctx context.Context, t *testing.T, db *gorm.DB, postgres *configv1.Postgres, submit domain.GatewaySubmit, lifetime time.Duration, directory string) {
	t.Helper()
	feed, publisher, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, publisher.Key)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, lifecycleToken(submit.Input.Authority).Key)
	ca := testcert.New(t)
	naming := businessNaming()
	naming.CommandTimeout = durationpb.New(3 * time.Second)
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	stream := businessStreamWithLimits(ctx, t, businessStreamLimits{
		Bytes:    128 * 1024 * 1024,
		Messages: 100000,
		Age:      time.Hour,
	})
	stream.CommandTimeout = durationpb.New(3 * time.Second)
	market := marketProcessConfig(t, ca, postgres, naming, feed.ID, "soak", "99.99", "100.01")
	market.Business.Market.LeaseTtl = durationpb.New(10 * time.Second)
	execution := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	execution.Business.ExecutionLeaseTtl = durationpb.New(10 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	execution.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	strategy := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "soak")
	strategy.Business.Strategy.LeaseTtl = durationpb.New(10 * time.Second)
	processes := []*configv1.ServiceConfig{market, execution, businessProcessConfig(t, ca, postgres, stream, naming, "portfolio"), strategy}
	pids := map[int]struct{}{os.Getpid(): {}}
	for _, cfg := range processes {
		cfg.Business.OperationTimeout = durationpb.New(3 * time.Second)
		if cfg.GrpcClients != nil {
			cfg.GrpcClients.RequestTimeout = durationpb.New(3 * time.Second)
		}
		pid := startBusinessProcessFor(ctx, t, cfg, observer, lifetime, directory)
		if _, exists := pids[pid]; exists {
			t.Fatal("business roles must run in separate native processes")
		}
		pids[pid] = struct{}{}
	}
}
