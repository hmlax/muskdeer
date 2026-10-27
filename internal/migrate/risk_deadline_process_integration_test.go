//go:build integration

package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestRiskProcessDeadlineExitsWithoutOpeningGrantUnderDatabaseLock(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).
		Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).UpdateColumn("allow_new_risk", false).Error; err != nil {
		t.Fatal(err)
	}
	cfg := businessProcessConfig(t, testcert.New(t), postgres, businessStream(ctx, t), businessNaming(), "execution")
	cfg.Business.OperationTimeout = durationpb.New(200 * time.Millisecond)
	cfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	cfg.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "risk-deadline.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// Keep the evaluator's first strategy lock unavailable beyond its native
	// budget. It must terminate and withdraw rather than publish a stale grant.
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		var strategy modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&strategy, submit.Input.Authority.StrategyID).Error; err != nil {
			return err
		}
		command := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", "muskdeer"), "-role", cfg.Name, "-config", path)
		output, runErr := command.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("blocked risk process did not fail closed: %v, %s", runErr, output)
		}
		if !hasDeadlineProcessDiagnostic(output) {
			t.Fatalf("risk process did not expose safe native deadline classification: %s", output)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).
		Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&risk).Error; err != nil {
		t.Fatal(err)
	}
	if risk.AllowNewRisk || risk.Revision != 1 {
		t.Fatal("timed-out evaluator wrote or opened a grant")
	}
}

func hasDeadlineProcessDiagnostic(output []byte) bool {
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		var record struct {
			Level     string `json:"level"`
			Message   string `json:"msg"`
			CauseKind string `json:"cause_kind"`
		}
		if json.Unmarshal(line, &record) == nil && record.Level == "ERROR" && record.Message == "service failed" && record.CauseKind == "deadline_exceeded" {
			return true
		}
	}
	return false
}
