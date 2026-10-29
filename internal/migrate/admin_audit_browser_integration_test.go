//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

const auditBrowserTextReason = `审计原因 <img src=x onerror="window.__auditExecuted=true"> 保留原文`

type auditBrowserRecords struct {
	TextID     int64
	HighID     int64
	StrategyID int64
}

func TestAdminAuditBrowserReadsExactHistoryAndRejectsExpiredSession(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	records := seedAuditBrowserHistory(ctx, t, db)
	var original []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&original).Error; err != nil {
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
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	runAuditBrowser(ctx, t, base, records)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 27)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ExecutionShardLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 1)
	assertAuditHTTPBalancesUnchanged(ctx, t, db, original)
}

func seedAuditBrowserHistory(ctx context.Context, t *testing.T, db *gorm.DB) auditBrowserRecords {
	t.Helper()
	actor, strategyID := auditHTTPHistoryFixture(ctx, t, db)
	instance := publicationInstance(ctx, t, db, strategyID)
	submit := domain.GatewaySubmit{
		Input: domain.OrderInput{
			AccountID:    instance.AccountId,
			InstrumentID: instance.InstrumentId,
		},
	}
	creator, err := repository.NewAdminStrategyCreator(db)
	if err != nil {
		t.Fatal(err)
	}
	records := auditBrowserRecords{
		HighID:     9007199254740993,
		StrategyID: strategyID,
	}
	for index := range 20 {
		creation := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceBinance)
		creation.Key = "audit-browser-create-" + strconv.Itoa(index)
		creation.InstanceKey = creation.Key
		creation.Reason = "audit browser pagination history " + strconv.Itoa(index)
		if index == 0 {
			creation.Reason = auditBrowserTextReason
		}
		created, err := creator.Create(ctx, actor.Token, actor.CSRF, creation)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			var audit modelv1.AdminAudit
			if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).
				Where("resource = ? AND action = ?", domain.AdminAuditResourceStrategy+":"+strconv.FormatInt(created.StrategyID, 10), domain.AdminStrategyCreate).Take(&audit).Error; err != nil {
				t.Fatal(err)
			}
			records.TextID = audit.Id
		}
	}
	// Imported history is a public fixture for int64 display, not a new operation.
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(&modelv1.AdminAudit{
		Id:            records.HighID,
		Actor:         "historical-import",
		ActorRole:     string(domain.AdminRoleAdmin),
		Action:        domain.AdminStrategyPublish,
		Resource:      domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(strategyID, 10),
		Reason:        "historical import precision fixture",
		BeforeVersion: proto.Int64(9007199254740994),
		AfterVersion:  proto.Int64(9007199254740995),
	}).Error; err != nil {
		t.Fatal(err)
	}
	return records
}

func runAuditBrowser(ctx context.Context, t *testing.T, base string, records auditBrowserRecords) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-audit-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-audits")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_AUDIT_TEXT_ID="+strconv.FormatInt(records.TextID, 10), "MUSKDEER_BROWSER_AUDIT_HIGH_ID="+strconv.FormatInt(records.HighID, 10),
		"MUSKDEER_BROWSER_AUDIT_STRATEGY_ID="+strconv.FormatInt(records.StrategyID, 10), "MUSKDEER_BROWSER_AUDIT_TEXT="+auditBrowserTextReason)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual audit browser failed: %v\n%s", err, output)
	}
	t.Logf("actual audit browser passed; screenshots=%s", artifacts)
}
