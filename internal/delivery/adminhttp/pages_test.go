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
