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
	for _, path := range paths {
		hertz.GET(path, func(_ context.Context, request *app.RequestContext) {
			if path == "/strategies/:strategyID/order-cancellations/:cancellationID" {
				_, strategyValid := strategyID(request)
				_, idValid := cancellationPathID(request.Param("cancellationID"))
				if !strategyValid || !idValid {
					request.String(http.StatusNotFound, "not found")
					return
				}
			}
			if path == "/strategies/:strategyID" {
				if _, valid := strategyID(request); !valid {
					request.String(http.StatusNotFound, "not found")
					return
				}
			}
			if path == "/operations/:operationID" {
				if _, valid := operationID(request); !valid {
					request.String(http.StatusNotFound, "not found")
					return
				}
			}
			if path == "/audits/:auditID" {
				if _, valid := auditID(request); !valid {
					request.String(http.StatusNotFound, "not found")
					return
				}
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
