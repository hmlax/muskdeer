//go:build integration

package migrate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestMarketProcessPublicationDeadlineRollsBackAndWithdraws(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	feed, publisher, _, previous := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, publisher.Key)
	// Reviewed fault DDL in this test's disposable schema only. Reads and
	// constructor checks remain available; the first publication alone blocks.
	const delayedPublication = `CREATE FUNCTION test_delayed_publication() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_sleep(1);
    RETURN NEW;
END;
$$;
CREATE TRIGGER test_delayed_publication BEFORE UPDATE ON market_snapshots
    FOR EACH ROW EXECUTE FUNCTION test_delayed_publication();`
	if err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Exec(delayedPublication).Error; err != nil {
		t.Fatal(err)
	}
	cfg := marketProcessConfig(t, testcert.New(t), postgres, businessNaming(), feed.ID, "publication-deadline", "99.99", "100.01")
	cfg.Business.OperationTimeout = durationpb.New(200 * time.Millisecond)
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "market-deadline.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", "muskdeer"), "-role", cfg.Name, "-config", path)
	output, runErr := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() != 1 || !hasDeadlineProcessDiagnostic(output) {
		t.Fatalf("blocked market publication did not terminate with native deadline: %v, %s", runErr, output)
	}
	var stored modelv1.MarketSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.MarketSnapshot{}).Where("market_feed_id = ?", feed.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Revision != previous.Revision || stored.PublisherOwnerId != previous.OwnerID {
		t.Fatal("publication timeout committed a newer healthy snapshot")
	}
	var lease modelv1.MarketPublisherLease
	if err := db.WithContext(ctx).Model(&modelv1.MarketPublisherLease{}).Where("market_feed_id = ?", feed.ID).First(&lease).Error; err != nil {
		t.Fatal(err)
	}
	if lease.ExpiresAt != 0 || lease.Epoch <= publisher.Epoch {
		t.Fatal("timed-out publisher retained its authority")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorOrder{}, 0)
}
