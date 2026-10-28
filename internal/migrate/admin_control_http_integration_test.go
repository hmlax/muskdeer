//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminControlHTTPAuthenticatesAndCompletesThroughRealOwnedProcesses(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, _, _, instances := adminStrategiesFixture(ctx, t, db)
	adminControlFixture(ctx, t, db)
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
	csrf := nativeControlHTTPLogin(ctx, t, client, jar, base, "control-operator", domain.AdminRoleOperator)
	path := "/api/v1/strategies/" + strconv.FormatInt(instances[0].Id, 10) + "/control"
	startBody := nativeControlHTTPBody{
		Action:           domain.AdminControlStart,
		ExpectedRevision: "1",
		Reason:           "actual same-origin operator start",
	}
	accepted := nativeControlHTTPPost(ctx, t, client, base, path, "http-native-start-0001", csrf, startBody, 202)
	replayed := nativeControlHTTPPost(ctx, t, client, base, path, "http-native-start-0001", csrf, startBody, 202)
	if accepted.Id != replayed.Id || accepted.State != string(domain.AdminOperationAccepted) || accepted.CompletedAtMs != nil || accepted.Observation != nil {
		t.Fatal("actual HTTP accepted control fabricated runtime success or duplicated effect")
	}
	changed := startBody
	changed.Reason = "different body on the same original key"
	nativeControlHTTPPost(ctx, t, client, base, path, "http-native-start-0001", csrf, changed, 409)
	nativeControlHTTPPost(ctx, t, client, base, path, "http-native-stale-0001", csrf, startBody, 409)
	stopBody := nativeControlHTTPBody{
		Action:           domain.AdminControlStop,
		ExpectedRevision: "2",
		Reason:           "actual operator stop replaces start",
	}
	stop := nativeControlHTTPPost(ctx, t, client, base, path, "http-native-stop-0001", csrf, stopBody, 202)
	old := nativeControlHTTPPost(ctx, t, client, base, path, "http-native-start-0001", csrf, startBody, 200)
	if old.Id != accepted.Id || old.State != string(domain.AdminOperationSuperseded) {
		t.Fatal("actual HTTP revived a superseded key")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instances[0].Id).
		Select("State", "Revision").Updates(&modelv1.StrategyInstance{
		State:    string(domain.StrategyStateStopped),
		Revision: 4,
	}).Error; err != nil {
		t.Fatal(err)
	}
	waitNativeHTTPControlCompletion(ctx, t, client, base, stop.Id)
	csrf = nativeControlHTTPLogin(ctx, t, client, jar, base, "strategy-viewer", domain.AdminRoleViewer)
	stopBody.ExpectedRevision = "4"
	nativeControlHTTPPost(ctx, t, client, base, path, "http-native-viewer-denied", csrf, stopBody, 403)
	var result strategyv1.StrategyOperation
	operationPath := "/api/v1/operations/" + strconv.FormatInt(stop.Id, 10)
	readNativeStrategyProto(ctx, t, client, base, operationPath, &result)
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	oldCookies := jar.Cookies(parsed)
	logout := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := logout.Body.Close(); err != nil || logout.StatusCode != 204 {
		t.Fatal("actual control viewer logout failed")
	}
	jar.SetCookies(parsed, oldCookies)
	revoked := nativeAdminRequest(ctx, t, client, base, "GET", operationPath, "", "", "")
	if err := revoked.Body.Close(); err != nil || revoked.StatusCode != 401 {
		t.Fatal("actual business operation query reused a revoked bearer")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

type nativeControlHTTPBody struct {
	Action           domain.AdminControlAction `json:"action"`
	ExpectedRevision string                    `json:"expected_revision"`
	Reason           string                    `json:"reason"`
}

func nativeControlHTTPPost(ctx context.Context, t *testing.T, client *http.Client, base, path, key, csrf string, input nativeControlHTTPBody, expected int) *strategyv1.StrategyOperation {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", path, string(data), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: key,
	})
	body, readErr := io.ReadAll(io.LimitReader(reply.Body, 4096))
	closeErr := reply.Body.Close()
	if readErr != nil || closeErr != nil || reply.StatusCode != expected {
		t.Fatalf("native control HTTP status mismatch: actual=%d expected=%d read_error=%v close_error=%v", reply.StatusCode, expected, readErr, closeErr)
	}
	if expected >= 400 {
		return nil
	}
	var operation strategyv1.StrategyOperation
	if err := protojson.Unmarshal(body, &operation); err != nil || reply.Header.Get("Location") != "/api/v1/operations/"+strconv.FormatInt(operation.Id, 10) {
		t.Fatal("native control HTTP response lost its typed receipt/location")
	}
	return &operation
}

func nativeControlHTTPLogin(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, username string, role domain.AdminRole) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: username,
		Password: adminFixturePassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", string(data), base, "")
	var session struct {
		Session struct {
			Role     domain.AdminRole `json:"role"`
			Controls bool             `json:"strategy_controls_enabled"`
		} `json:"session"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(reply.Body, 4096)).Decode(&session)
	closeErr := reply.Body.Close()
	if reply.StatusCode != 200 || decodeErr != nil || closeErr != nil || session.Session.Role != role || !session.Session.Controls {
		t.Fatal("real password login or explicit control capability failed")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "muskdeer_csrf" {
			return cookie.Value
		}
	}
	t.Fatal("real login did not deliver independent CSRF")
	return ""
}

func waitNativeHTTPControlCompletion(ctx context.Context, t *testing.T, client *http.Client, base string, id int64) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var operation strategyv1.StrategyOperation
		readNativeStrategyProto(call, t, client, base, "/api/v1/operations/"+strconv.FormatInt(id, 10), &operation)
		if operation.State == string(domain.AdminOperationSucceeded) {
			if operation.CompletedAtMs == nil || operation.Observation.GetState() != string(domain.StrategyStateStopped) || operation.Observation.StrategyRevision != 4 {
				t.Fatal("actual HTTP result lost its persisted runtime proof")
			}
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("actual owned worker did not publish final operation result")
		}
	}
}
