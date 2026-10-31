package adminhttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"

	adminui "github.com/hmlax/muskdeer/web/admin"
)

func (r *Routes) pages(hertz *server.Hertz) {
	paths := []string{"/", "/login", "/session"}
	if r.strategies != nil {
		paths = append(paths, "/strategies", "/strategies/:strategyID")
	}
	if r.controls != nil {
		paths = append(paths, "/operations/:operationID")
	}
	if r.audits != nil {
		paths = append(paths, "/audits", "/audits/:auditID")
	}
	if r.cancellationQueries != nil {
		paths = append(paths, "/strategies/:strategyID/order-cancellations/:cancellationID")
	}
	if r.markets != nil {
		paths = append(paths, "/markets", "/markets/:feedID")
	}
	if r.trades != nil {
		paths = append(paths, "/strategies/:strategyID/trades", "/strategies/:strategyID/trades/:tradeID")
	}
	if r.portfolios != nil {
		paths = append(paths, "/portfolios", "/portfolios/:portfolioID")
	}
	if r.riskEvents != nil {
		paths = append(paths, "/risk-events", "/risk-events/:eventID")
	}
	if r.overview != nil && r.services != nil {
		paths = append(paths, "/overview")
	}
	for _, path := range paths {
		hertz.GET(path, func(_ context.Context, request *app.RequestContext) {
			if !validPageIdentity(path, request) {
				request.String(http.StatusNotFound, "not found")
				return
			}
			page, err := adminui.File("index.html")
			if err != nil {
				request.String(http.StatusServiceUnavailable, "page unavailable")
				return
			}
			request.Data(http.StatusOK, "text/html; charset=utf-8", page)
		})
	}
	hertz.GET("/assets/*filepath", func(_ context.Context, request *app.RequestContext) {
		name := "assets/" + strings.TrimPrefix(request.Param("filepath"), "/")
		data, err := adminui.File(name)
		if err != nil {
			request.String(http.StatusNotFound, "not found")
			return
		}
		contentType := "application/octet-stream"
		switch {
		case strings.HasSuffix(name, ".js"):
			contentType = "text/javascript; charset=utf-8"
		case strings.HasSuffix(name, ".css"):
			contentType = "text/css; charset=utf-8"
		}
		request.Data(http.StatusOK, contentType, data)
	})
}

func validPageIdentity(path string, request *app.RequestContext) bool {
	switch path {
	case "/risk-events/:eventID":
		_, valid := riskEventPathID(request.Param("eventID"))
		return valid
	case "/portfolios/:portfolioID":
		_, valid := portfolioPathID(request.Param("portfolioID"))
		return valid
	case "/markets/:feedID":
		_, valid := marketPathID(request.Param("feedID"))
		return valid
	case "/strategies/:strategyID/trades", "/strategies/:strategyID/trades/:tradeID":
		_, strategyValid := strategyID(request)
		idValid := true
		if path == "/strategies/:strategyID/trades/:tradeID" {
			_, idValid = tradePathID(request.Param("tradeID"))
		}
		return strategyValid && idValid
	case "/strategies/:strategyID/order-cancellations/:cancellationID":
		_, strategyValid := strategyID(request)
		_, idValid := cancellationPathID(request.Param("cancellationID"))
		return strategyValid && idValid
	case "/strategies/:strategyID":
		_, valid := strategyID(request)
		return valid
	case "/operations/:operationID":
		_, valid := operationID(request)
		return valid
	case "/audits/:auditID":
		_, valid := auditID(request)
		return valid
	default:
		return true
	}
}
