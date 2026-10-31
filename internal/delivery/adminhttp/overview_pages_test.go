package adminhttp

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func TestOverviewHTMLRequiresBothActualQueryCapabilities(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		routes, err := New(&authFixture{}, testOrigin, true)
		if err != nil {
			t.Fatal(err)
		}
		routes.overview = &overviewHTTPClient{}
		if enabled {
			routes.services = &servicesHTTPReader{}
		}
		hertz := server.New()
		routes.Register(hertz)
		reply := perform(hertz, "GET", "/overview?role=api", "")
		if enabled && (reply.Code != 200 || !strings.Contains(reply.Body.String(), "<html")) || !enabled && reply.Code != 404 {
			t.Fatal("overview HTML ignored its actual query capabilities", reply.Code)
		}
	}
}
