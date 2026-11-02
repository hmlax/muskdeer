//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/adminactor"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestTwoAppsUseIndependentEntrypointsAndAuthorizeOnlyInAdmin(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, auth, login, instances := adminStrategiesFixture(ctx, t, db)
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
	operator := businessProcessConfig(t, ca, postgres, nil, naming, "operator")
	operator.GrpcSecurity.AllowedPeerServices = []string{rpcsecurity.ServiceAdmin}
	operator.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	operatorPID := startBusinessProcess(ctx, t, operator, observer)
	admin := &configv1.ServiceConfig{
		Name:            "admin",
		InstanceId:      "two-apps-admin",
		Version:         "test",
		StartupTimeout:  durationpb.New(5 * time.Second),
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Http: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		Monitoring: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
		Postgres:    proto.CloneOf(postgres),
		Nacos:       proto.CloneOf(naming),
		GrpcClients: businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceAdmin, false)),
		AdminHttp: &configv1.AdminHTTP{
			Enabled:                 true,
			PublicOrigin:            "http://127.0.0.1:0",
			AllowInsecureLoopback:   true,
			StrategyQueriesEnabled:  true,
			StrategyControlsEnabled: true,
		},
	}
	admin.GrpcClients.AllowedServices = []string{rpcsecurity.ServiceOperator}
	directory := t.TempDir()
	path := filepath.Join(directory, "admin.json")
	data, err := protojson.Marshal(admin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	base := startNativeAdminAPI(ctx, t, path, directory)
	if operatorPID == os.Getpid() {
		t.Fatal("operator reused the test process")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 3 * time.Second,
	}
	t.Cleanup(client.CloseIdleConnections)
	httpCalls := &twoAppsHTTP{
		base:   base,
		client: client,
	}
	request := func(method, path, body, csrf, key string) (int, []byte) {
		return httpCalls.request(ctx, t, method, path, body, csrf, key)
	}
	loginHTTP := func(username string) string { return httpCalls.login(ctx, t, username) }

	csrf := loginHTTP(login.Principal.Username)
	if os.Getenv("MUSKDEER_TWO_APPS_BROWSER") == "1" {
		verifyTwoAppsReact(ctx, t, base, login.Principal.Username)
	}
	httpCalls.assertOverview(ctx, t)

	if code, body := request("GET", "/api/v1/strategies/"+strconv.FormatInt(instances[1].Id, 10), "", "", ""); code != 200 || !strings.Contains(string(body), "0.123456789012345678") {
		t.Fatalf("HTTP to operator lost business data/precision: status=%d body=%s", code, body)
	}
	target := "/api/v1/strategies/" + strconv.FormatInt(instances[0].Id, 10) + "/control"
	control := `{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"1","reason":"two apps test"}`
	if code, _ := request("POST", target, control, csrf, "two-apps-control-stop"); code != 403 {
		t.Fatalf("viewer management write: status=%d", code)
	}
	if _, err := adminProvisionRequest(ctx, t, auth, "app-operator", domain.AdminRoleOperator); err != nil {
		t.Fatal(err)
	}
	csrf = loginHTTP("app-operator")
	code, body := request("POST", target, control, csrf, "two-apps-control-stop")
	if code != 202 && code != 200 {
		t.Fatalf("admin-validated operator control: status=%d body=%s", code, body)
	}
	var receipt strategyv1.StrategyOperation
	if err := protojson.Unmarshal(body, &receipt); err != nil || receipt.Id <= 0 {
		t.Fatal("control receipt", err)
	}
	code, body = request("POST", target, control, csrf, "two-apps-control-stop")
	var replay strategyv1.StrategyOperation
	if err := protojson.Unmarshal(body, &replay); err != nil || (code != 202 && code != 200) || replay.Id != receipt.Id {
		t.Fatal("actor-scoped retry changed receipt", err)
	}
	if code, _ := request("POST", "/api/v1/auth/logout", "", csrf, ""); code != 204 {
		t.Fatal("logout failed")
	}
	if code, _ := request("GET", "/api/v1/strategies", "", "", ""); code != 401 {
		t.Fatal("admin reused revoked browser session")
	}
	// A previously validated audit actor is independent of current session state.
	// This proves operator no longer reads sessions or rechecks roles.
	if err := auth.Logout(ctx, login.Token, login.CSRF); err != nil {
		t.Fatal(err)
	}
	factory, err := rpcclient.New(ctx, admin.GrpcClients, rpcsecurity.ServiceAdmin, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceOperator})
	if err != nil {
		t.Fatal(err)
	}
	rpc := strategyv1.NewStrategyManagementClient(connection)
	bounded, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	actorContext := adminactor.Outgoing(adminactor.Bind(bounded, login.Principal))
	if page, err := rpc.ListStrategies(actorContext, &strategyv1.ListStrategiesRequest{Limit: 10}); err != nil || len(page.Strategies) != len(instances) {
		t.Fatal("operator repeated browser authorization", err)
	}
	if _, err := rpc.ListStrategies(bounded, &strategyv1.ListStrategiesRequest{
		Session: &strategyv1.AdminSession{Token: login.Token},
		Limit:   10,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("operator accepted browser credentials instead of admin audit identity", err)
	}
}

type twoAppsHTTP struct {
	base   string
	client *http.Client
}

func (a *twoAppsHTTP) request(ctx context.Context, t *testing.T, method, path, body, csrf, key string) (int, []byte) {
	t.Helper()
	call, err := http.NewRequestWithContext(ctx, method, a.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	call.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		call.Header.Set("Origin", a.base)
	}
	if csrf != "" {
		call.Header.Set("X-CSRF-Token", csrf)
	}
	if key != "" {
		call.Header.Set("Idempotency-Key", key)
	}
	reply, err := a.client.Do(call)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(reply.Body, 256*1024))
	if err != nil {
		t.Fatal(err)
	}
	if err := reply.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return reply.StatusCode, bodyBytes
}

func (a *twoAppsHTTP) login(ctx context.Context, t *testing.T, username string) string {
	t.Helper()
	code, body := a.request(ctx, t, "POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"`+adminFixturePassword+`"}`, "", "")
	if code != 200 {
		t.Fatalf("independent admin login: status=%d body=%s", code, body)
	}
	var session nativeSessionReply
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	call, _ := http.NewRequestWithContext(ctx, "GET", a.base, nil)
	for _, cookie := range a.client.Jar.Cookies(call.URL) {
		if cookie.Name == "muskdeer_csrf" {
			return cookie.Value
		}
	}
	t.Fatal("admin omitted CSRF")
	return ""
}

func verifyTwoAppsReact(ctx context.Context, t *testing.T, base, username string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(root, ".cache", "two-apps-browser")
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-two-apps")
	command.Dir = root
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_USERNAME="+username, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("React two app browser failed: %v %s", err, output)
	}
	t.Log("React two app browser passed; artifacts:", artifacts)
}

func (a *twoAppsHTTP) assertOverview(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, role := range []string{string(domain.AdminServiceAdmin), string(domain.AdminServiceOperator)} {
		if code, body := a.request(ctx, t, "GET", "/api/v1/overview/services?role="+role, "", "", ""); code != 200 || !strings.Contains(string(body), `"role":"`+role+`"`) || !strings.Contains(string(body), `"status":"READY"`) {
			t.Fatalf("two app service overview %s: status=%d body=%s", role, code, body)
		}
	}
}
