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

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminStrategyBrowserReadsRealVersionsAndSources(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, _, _, instances := adminStrategiesFixture(ctx, t, db)
	seedStrategyBrowserPages(ctx, t, db, instances[0])
	ca := testcert.New(t)
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, observer)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	runStrategyBrowser(ctx, t, base, instances, false)
	// Only this test's unfunded metadata is soft-deleted, after the real data
	// browser closes. The second browser must observe a genuinely empty DB.
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id > ?", 0).
		UpdateColumn("delete_at", time.Now().UTC().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	runStrategyBrowser(ctx, t, base, instances, true)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func seedStrategyBrowserPages(ctx context.Context, t *testing.T, db *gorm.DB, maker *modelv1.StrategyInstance) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		for index := range 19 {
			instance := identityInstance(maker.InstrumentId, fmt.Sprintf("page-grid-%02d", index+1), domain.StrategyTemplateGrid, domain.MarketSourceOKX)
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
				return err
			}
			version := storedParameterFixture(domain.StrategyTemplateGrid)
			version.InstanceId, version.MarketDataSource = instance.Id, string(domain.MarketSourceOKX)
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
				return err
			}
		}
		for version := int64(2); version <= 21; version++ {
			row := storedParameterFixture(domain.StrategyTemplateMarketMaker)
			row.InstanceId, row.Version = maker.Id, version
			row.MakerParameters.LevelNotional, row.MakerParameters.SideBudget = "123.123456789012345678", "1000"
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(row).Error; err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", maker.Id).
			Select("RequestedConfigVersion", "Revision").Updates(&modelv1.StrategyInstance{
			RequestedConfigVersion: 21,
			Revision:               2,
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
}

func runStrategyBrowser(ctx context.Context, t *testing.T, base string, instances []*modelv1.StrategyInstance, empty bool) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-strategy-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-strategies")
	command.Dir = root
	command.Env = append(os.Environ(),
		"MUSKDEER_BROWSER_ORIGIN="+base,
		"MUSKDEER_BROWSER_USERNAME=strategy-viewer",
		"MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword,
		"MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_MAKER_ID="+strconv.FormatInt(instances[0].Id, 10),
		"MUSKDEER_BROWSER_GRID_ID="+strconv.FormatInt(instances[1].Id, 10),
		"MUSKDEER_BROWSER_EMPTY="+strconv.FormatBool(empty),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual strategy browser failed: %v\n%s", err, output)
	}
	t.Logf("actual strategy browser passed; empty=%t screenshots=%s", empty, artifacts)
}
