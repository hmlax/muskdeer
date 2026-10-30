package adminhttp

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedPagesRefreshAndAssetsCannotSwallowAPIOrReadLocalFiles(t *testing.T) {
	hertz := httpFixture(t, &authFixture{})
	root := perform(hertz, "GET", "/", "")
	if root.Code != 200 || !strings.HasPrefix(string(root.Header().Peek("Content-Type")), "text/html") {
		t.Fatal("embedded administration entry missing")
	}
	for _, path := range []string{"/login", "/session"} {
		page := perform(hertz, "GET", path, "")
		if page.Code != 200 || !bytes.Equal(page.Body.Bytes(), root.Body.Bytes()) {
			t.Fatal("deep route refresh did not serve the same application")
		}
	}
	files := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+)"`).FindAllStringSubmatch(root.Body.String(), -1)
	if len(files) != 2 {
		t.Fatal("entry must reference its built script and stylesheet")
	}
	for _, file := range files {
		asset := perform(hertz, "GET", file[1], "")
		if asset.Code != 200 || asset.Body.Len() == 0 || string(asset.Header().Peek("X-Content-Type-Options")) != "nosniff" {
			t.Fatal("embedded application asset unavailable")
		}
	}
	for _, path := range []string{"/assets/missing.js", "/assets/../index.html", "/assets/../../go.mod", "/assets/%2e%2e/%2e%2e/go.mod", "/api/v1/missing"} {
		response := perform(hertz, "GET", path, "")
		if response.Code != 404 || strings.Contains(response.Body.String(), "<html") || strings.Contains(response.Body.String(), "module github.com") {
			t.Fatal("static fallback swallowed an API error or escaped embedded assets")
		}
	}
}

func TestAuditPagesRequireOptInAndCanonicalHistoryIdentity(t *testing.T) {
	hertz := auditHTTPFixture(t, &auditHTTPClient{})
	for _, path := range []string{"/audits", "/audits?actor=history-actor", "/audits/9007199254740993"} {
		page := perform(hertz, "GET", path, "")
		if page.Code != 200 || !strings.HasPrefix(string(page.Header().Peek("Content-Type")), "text/html") {
			t.Fatal("audit deep route refresh did not serve the embedded application", path)
		}
	}
	for _, path := range []string{"/audits/0", "/audits/01", "/audits/9223372036854775808", "/api/v1/audits/missing"} {
		page := perform(hertz, "GET", path, "")
		if page.Code == 200 || strings.Contains(page.Body.String(), "<html") {
			t.Fatal("audit page fallback swallowed invalid identity or an API error", path)
		}
	}
	disabled := auditHTTPFixture(t, nil)
	if page := perform(disabled, "GET", "/audits", ""); page.Code != 404 {
		t.Fatal("audit page ignored query opt-in")
	}
}

func TestCancellationPagesRequireQueriesAndBothCanonicalIdentities(t *testing.T) {
	hertz := cancellationHTTPFixture(t, &cancellationHTTPClient{}, nil)
	path := "/strategies/9007199254740994/order-cancellations/9007199254740993"
	if reply := perform(hertz, "GET", path, ""); reply.Code != 200 || !strings.HasPrefix(string(reply.Header().Peek("Content-Type")), "text/html") {
		t.Fatal("query-only cancellation deep route refresh did not serve the application", reply.Code)
	}
	for _, invalid := range []string{
		"/strategies/01/order-cancellations/1",
		"/strategies/1/order-cancellations/0",
		"/strategies/1/order-cancellations/01",
		"/strategies/1/order-cancellations/9223372036854775808",
	} {
		if reply := perform(hertz, "GET", invalid, ""); reply.Code != 404 {
			t.Fatal("cancellation page accepted noncanonical scope or identity", reply.Code)
		}
	}
	if reply := perform(cancellationHTTPFixture(t, nil, nil), "GET", path, ""); reply.Code != 404 {
		t.Fatal("disabled cancellation query exposed a page", reply.Code)
	}
}
