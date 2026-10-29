//go:build integration

package migrate

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminAuditHTTPReadsActualHistoryAndRejectsRevokedSession(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, strategyID := auditHTTPHistoryFixture(ctx, t, db)
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
	resource := domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(strategyID, 10)
	assertNativeAuditRPC(ctx, t, ca, discovery, actor.Token, resource)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		MaxIdleConns:    2,
		MaxConnsPerHost: 2,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	login := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", `{"username":"audit-viewer","password":"`+adminFixturePassword+`"}`, base, "")
	if err := login.Body.Close(); err != nil || login.StatusCode != 200 {
		t.Fatal("actual audit viewer password login failed", err)
	}
	firstID := assertNativeAuditHTTP(ctx, t, client, base, resource)
	assertNativeAuditLogout(ctx, t, client, base, jar, firstID)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 5)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ExecutionShardLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 1)
	assertAuditHTTPBalancesUnchanged(ctx, t, db, original)
	instance := publicationInstance(ctx, t, db, strategyID)
	if instance.State != string(domain.StrategyStateCreated) || instance.ActiveConfigVersion != nil || instance.RuntimeOwnerEpoch != 0 {
		t.Fatal("read-only audit delivery created runtime authority")
	}
}

func assertAuditHTTPBalancesUnchanged(ctx context.Context, t *testing.T, db *gorm.DB, original []*modelv1.SimulatorBalance) {
	t.Helper()
	var after []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&after).Error; err != nil || len(after) != len(original) {
		t.Fatal("audit delivery changed core balance rows", err)
	}
	for index := range original {
		if !proto.Equal(original[index], after[index]) {
			t.Fatal("audit delivery changed core funds or balance revision")
		}
	}
}

func auditHTTPHistoryFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, int64) {
	t.Helper()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	creation := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceBinance)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, creation)
	if err != nil {
		t.Fatal(err)
	}
	other := adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceOKX)
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, other); err != nil {
		t.Fatal(err)
	}
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(creation, created.StrategyID, created.Revision, 1)
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, publication); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "audit-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return actor, created.StrategyID
}

func assertNativeAuditRPC(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token, resource string) {
	t.Helper()
	api := strategyv1.NewStrategyAuditClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI))
	request := &strategyv1.ListAuditsRequest{
		Session:  &strategyv1.AdminSession{Token: token},
		Limit:    2,
		Resource: resource,
	}
	page, err := api.ListAudits(ctx, request)
	if err != nil || len(page.Audits) != 2 || page.Audits[0].Action != domain.AdminStrategyCreate || page.Audits[1].Action != domain.AdminStrategyPublish {
		t.Fatal("actual audit RPC lost exact management history", err)
	}
	other := strategyv1.NewStrategyAuditClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio))
	if _, err := other.ListAudits(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API valid certificate reached actual audit history", err)
	}
}

func assertNativeAuditHTTP(ctx context.Context, t *testing.T, client *http.Client, base, resource string) int64 {
	t.Helper()
	path := "/api/v1/audits?limit=1&resource=" + url.QueryEscape(resource)
	var first strategyv1.ListAuditsReply
	readNativeStrategyProto(ctx, t, client, base, path, &first)
	if len(first.Audits) != 1 || first.NextAfterId != first.Audits[0].Id || first.Audits[0].BeforeVersion != nil || first.Audits[0].GetAfterVersion() != 1 || first.Audits[0].ActorRole != string(domain.AdminRoleOperator) {
		t.Fatal("actual audit HTTP lost creation presence or historical operator role")
	}
	var second strategyv1.ListAuditsReply
	readNativeStrategyProto(ctx, t, client, base, path+"&after_id="+strconv.FormatInt(first.NextAfterId, 10), &second)
	if len(second.Audits) != 1 || second.NextAfterId != 0 || second.Audits[0].Action != domain.AdminStrategyPublish || second.Audits[0].Resource != resource || second.Audits[0].GetBeforeVersion() != 1 || second.Audits[0].GetAfterVersion() != 2 {
		t.Fatal("actual audit HTTP crossed resource or changed publication revisions")
	}
	var detail strategyv1.GetAuditReply
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/audits/"+strconv.FormatInt(second.Audits[0].Id, 10), &detail)
	if detail.Audit == nil || detail.Audit.Id != second.Audits[0].Id || detail.Audit.Reason != second.Audits[0].Reason {
		t.Fatal("actual audit detail changed persisted reason or identity")
	}
	var filtered strategyv1.ListAuditsReply
	readNativeStrategyProto(ctx, t, client, base, path+"&actor=control-operator&action="+domain.AdminStrategyPublish, &filtered)
	if len(filtered.Audits) != 1 || filtered.Audits[0].Id != detail.Audit.Id || filtered.NextAfterId != 0 {
		t.Fatal("actual audit HTTP ignored combined exact filters")
	}
	return first.Audits[0].Id
}

func assertNativeAuditLogout(ctx context.Context, t *testing.T, client *http.Client, base string, jar *cookiejar.Jar, id int64) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	var csrf string
	for _, cookie := range cookies {
		if cookie.Name == "muskdeer_csrf" {
			csrf = cookie.Value
		}
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual audit viewer logout failed", err)
	}
	for _, path := range []string{"/api/v1/audits", "/api/v1/audits/" + strconv.FormatInt(id, 10)} {
		jar.SetCookies(parsed, cookies)
		reply := nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
			t.Fatal("actual audit HTTP accepted a restored revoked bearer", path, err)
		}
	}
}
