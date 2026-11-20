package router

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/swaggest/swgui/v5/static"

	"github.com/hmlax/muskdeer/admin/internal/transport"
	"github.com/hmlax/muskdeer/deer/management/apidocs"
)

const swaggerPage = `<!doctype html><html><head><meta charset="utf-8"><title>Go Admin API</title><link rel="stylesheet" href="/docs/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="/docs/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/docs/openapi.yaml',dom_id:'#swagger-ui',deepLinking:true,persistAuthorization:false});</script></body></html>`

func RegisterSwagger(router *transport.Router) {
	router.GET("/docs", func(_ context.Context, c *app.RequestContext) {
		c.Redirect(http.StatusTemporaryRedirect, []byte("/docs/"))
	})
	router.GET("/docs/*asset", func(ctx context.Context, c *app.RequestContext) {
		name := strings.TrimPrefix(c.Param("asset"), "/")
		switch name {
		case "", "index.html":
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerPage))
			return
		case "openapi.yaml":
			c.Data(http.StatusOK, "application/yaml", apidocs.OpenAPIData)
			return
		}
		if path.Base(name) != name {
			c.SetStatusCode(http.StatusNotFound)
			return
		}
		file, err := static.FS.Open(name)
		compressed := false
		if err != nil {
			file, err = static.FS.Open(name + ".gz")
			compressed = err == nil
		}
		if err != nil {
			c.SetStatusCode(http.StatusNotFound)
			return
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				slog.Error("close API documentation asset", "error", closeErr)
			}
		}()
		data, err := io.ReadAll(file)
		if err != nil {
			slog.ErrorContext(ctx, "read API documentation asset", "error", err)
			transport.FailRequest(c, err)
			return
		}
		if compressed {
			if strings.Contains(string(c.Request.Header.Peek("Accept-Encoding")), "gzip") {
				c.Response.Header.Set("Content-Encoding", "gzip")
			} else {
				reader, err := gzip.NewReader(bytes.NewReader(data))
				if err != nil {
					transport.FailRequest(c, err)
					return
				}
				data, err = io.ReadAll(reader)
				closeErr := reader.Close()
				if err != nil {
					transport.FailRequest(c, err)
					return
				}
				if closeErr != nil {
					transport.FailRequest(c, closeErr)
					return
				}
			}
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		c.Data(http.StatusOK, contentType, data)
	})
}
