//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

type marketBrowserFeeds struct {
	Binance domain.MarketFeedDefinition
	OKX     domain.MarketFeedDefinition
	Empty   domain.MarketFeedDefinition
}

func TestAdminMarketBrowserReadsIndependentProcessesAndRetainsSourceOnFailure(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	feeds := seedMarketBrowserFeeds(ctx, t, db)
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	binance := marketProcessConfig(t, ca, postgres, naming, feeds.Binance.ID, "browser-binance", "99.999999999999999999", "100")
	binancePID := startBusinessProcessFor(ctx, t, binance, discovery, 90*time.Second, "")
	okx := marketProcessConfig(t, ca, postgres, naming, feeds.OKX.ID, "browser-okx", "199.99", "200.01")
	startBusinessProcessFor(ctx, t, okx, discovery, 90*time.Second, "")
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcessFor(ctx, t, strategy, discovery, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, false, 90*time.Second)
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		t.Fatal(err)
	}
	waitMarketBrowserQuality(ctx, t, snapshots, feeds.Binance.ID, domain.MarketQualityHealthy)
	waitMarketBrowserQuality(ctx, t, snapshots, feeds.OKX.ID, domain.MarketQualityHealthy)
	runMarketBrowserPhase(ctx, t, base, feeds, "healthy")
	process, err := os.FindProcess(binancePID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitMarketBrowserQuality(ctx, t, snapshots, feeds.Binance.ID, domain.MarketQualityUnavailable)
	before := adminMarketStoredSnapshot(ctx, t, db, feeds.Binance.ID)
	runMarketBrowserPhase(ctx, t, base, feeds, "degraded")
	if !proto.Equal(before, adminMarketStoredSnapshot(ctx, t, db, feeds.Binance.ID)) {
		t.Fatal("browser read republished the stopped source")
	}
	waitMarketBrowserQuality(ctx, t, snapshots, feeds.OKX.ID, domain.MarketQualityHealthy)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketFeed{}, 3)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketSnapshot{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 0)
}

func seedMarketBrowserFeeds(ctx context.Context, t *testing.T, db *gorm.DB) marketBrowserFeeds {
	t.Helper()
	feeds := catalogFeedFixtures(ctx, t, db, marketInstrumentFixture(ctx, t, db))
	other := identityInstrument()
	other.BaseAsset, other.Symbol = "ETH", "ETH-USDT"
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(other).Error; err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	input := feeds[0]
	input.ID, input.InstrumentID, input.Symbol = 0, other.Id, "ETHUSDT"
	empty, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return marketBrowserFeeds{
		Binance: feeds[0],
		OKX:     feeds[1],
		Empty:   empty,
	}
}

func waitMarketBrowserQuality(ctx context.Context, t *testing.T, store domain.MarketRepository, id int64, quality domain.MarketQuality) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		value, err := store.Get(call, id)
		if err == nil && value.Reading.Quality == quality && (quality != domain.MarketQualityUnavailable || value.AuthorityUntil.IsZero()) {
			return
		}
		select {
		case <-call.Done():
			t.Fatal("actual market did not reach the expected fenced quality", call.Err())
		case <-ticks.C:
		}
	}
}

func runMarketBrowserPhase(ctx context.Context, t *testing.T, base string, feeds marketBrowserFeeds, phase string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-market-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-markets")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_MARKET_PHASE="+phase, "MUSKDEER_BROWSER_MARKET_BINANCE="+strconv.FormatInt(feeds.Binance.ID, 10),
		"MUSKDEER_BROWSER_MARKET_OKX="+strconv.FormatInt(feeds.OKX.ID, 10), "MUSKDEER_BROWSER_MARKET_EMPTY="+strconv.FormatInt(feeds.Empty.ID, 10),
		"MUSKDEER_BROWSER_MARKET_INSTRUMENT="+strconv.FormatInt(feeds.Binance.InstrumentID, 10))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual market browser phase=%s failed: %v\n%s", phase, err, output)
	}
	t.Logf("actual market browser phase=%s passed; screenshots=%s", phase, artifacts)
}
