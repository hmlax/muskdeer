//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminPublicationBrowserUsesActualVersionsAndPreservesUnknownRequest(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	var environment []string
	var instances []*modelv1.StrategyInstance
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateGrid, domain.StrategyTemplateMarketMaker} {
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			request := adminCreationRequest(t, submit, template, source)
			receipt, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
			if err != nil {
				t.Fatal(err)
			}
			environment = append(environment, "MUSKDEER_BROWSER_"+strings.ToUpper(string(template))+"_"+strings.ToUpper(string(source))+"_ID="+strconv.FormatInt(receipt.StrategyID, 10))
			instances = append(instances, publicationInstance(ctx, t, db, receipt.StrategyID))
		}
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "publication-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var balances []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&balances).Error; err != nil {
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
	parent := filepath.Join(root, ".cache", "admin-publication-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-publication")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts)
	command.Env = append(command.Env, environment...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual publication browser failed: %v\n%s", err, output)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 5)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 10)
	for index, instance := range instances {
		actual := publicationInstance(ctx, t, db, instance.Id)
		version := int64(2)
		if index == 0 {
			version = 3
		}
		if actual.RequestedConfigVersion != version || actual.Revision != version || actual.MarketDataSource == instance.MarketDataSource || actual.State != string(domain.StrategyStateCreated) || actual.DesiredState != string(domain.StrategyStateStopped) || actual.RuntimeOwnerEpoch != 0 || actual.ActiveConfigVersion != nil {
			t.Fatal("publication browser changed runtime/target or lost requested source/version")
		}
	}
	var retained []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&retained).Error; err != nil || len(retained) != len(balances) {
		t.Fatal("publication browser changed the core balance set", err)
	}
	for index := range balances {
		if !proto.Equal(balances[index], retained[index]) {
			t.Fatal("publication browser changed core funds")
		}
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	t.Logf("actual publication browser passed; screenshots=%s", artifacts)
}
