package bootstrap

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	"github.com/cloudwego/hertz/pkg/common/ut"
)

type captureStream struct {
	bytes.Buffer
	flushed bool
}

func (w *captureStream) Flush() error    { w.flushed = true; return nil }
func (w *captureStream) Finalize() error { return nil }

// serveNative exercises Hertz's engine; net/http is used only for test wire assertions.
func serveNative(router *Router, out *httptest.ResponseRecorder, request *http.Request) {
	headers := make([]ut.Header, 0)
	for key, values := range request.Header {
		for _, value := range values {
			headers = append(headers, ut.Header{
				Key:   key,
				Value: value,
			})
		}
	}
	var body *ut.Body
	if request.Body != nil {
		body = &ut.Body{
			Body: request.Body,
			Len:  int(request.ContentLength),
		}
	}
	c := ut.CreateUtRequestContext(request.Method, request.URL.String(), body, headers...)
	stream := new(captureStream)
	if request.URL.Path == "/events" {
		c.Response.HijackWriter(stream)
	}
	router.ServeHTTP(request.Context(), c)
	c.Response.Header.VisitAll(func(key, value []byte) { out.Header().Add(string(key), string(value)) })
	out.WriteHeader(c.Response.StatusCode())
	if stream.Len() > 0 {
		_, _ = out.Write(stream.Bytes())
	} else {
		_, _ = out.Write(c.Response.Body())
	}
	if stream.flushed {
		out.Flush()
	}
}
