//go:build integration && browser

package migrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminCreationBrowserUsesRealCatalogAndOriginalRequestKey(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	var accounts []*modelv1.SimulatorAccount
	var instruments []*modelv1.Instrument
	for index := 1; index <= 101; index++ {
		accounts = append(accounts, &modelv1.SimulatorAccount{
			AccountId:    fmt.Sprintf("creation-page-account-%03d", index),
			SubjectId:    "creation-page-subject",
			QuoteFeeRate: "0",
			Revision:     1,
		})
		instrument := identityInstrument()
		instrument.Symbol = fmt.Sprintf("PAGE%03d-USDT", index)
		instrument.BaseAsset = fmt.Sprintf("PAGE%03d", index)
		instrument.Status = domain.InstrumentStatusHalted
		instruments = append(instruments, instrument)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instruments).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "creation-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var before []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyManagementAPI(ctx, t, ca, postgres, naming, true)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-creation-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-creation")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(),
		"MUSKDEER_BROWSER_ORIGIN="+base,
		"MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword,
		"MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_ACCOUNT="+submit.Input.AccountID,
		"MUSKDEER_BROWSER_INSTRUMENT="+strconv.FormatInt(submit.Input.InstrumentID, 10),
		"MUSKDEER_BROWSER_CODE="+identityFixtureCode,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual creation browser failed: %v\n%s", err, output)
	}
	var rows []*modelv1.AdminStrategyCreation
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).Order("id").Find(&rows).Error; err != nil || len(rows) != 4 {
		t.Fatal("browser creation did not persist four unique receipts", err)
	}
	for _, receipt := range rows {
		var instance modelv1.StrategyInstance
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", receipt.StrategyInstanceId).Take(&instance).Error; err != nil || instance.DesiredRunning || instance.State != string(domain.StrategyStateCreated) || instance.RuntimeOwnerEpoch != 0 {
			t.Fatal("browser creation requested running or fabricated an owner", err)
		}
		var budget modelv1.StrategyBudget
		if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", instance.Id).Take(&budget).Error; err != nil {
			t.Fatal(err)
		}
		reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil || !reserved.IsZero() {
			t.Fatal("browser creation reserved trading funds", err)
		}
	}
	var after []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id").Find(&after).Error; err != nil || len(after) != len(before) {
		t.Fatal("browser creation changed core balance set", err)
	}
	for i := range before {
		if !proto.Equal(before[i], after[i]) {
			t.Fatal("browser creation changed actual core funds")
		}
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	t.Logf("actual creation browser passed; screenshots=%s", artifacts)
}
