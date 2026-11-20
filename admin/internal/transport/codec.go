package transport

import (
	"bytes"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	kerrors "github.com/go-kratos/kratos/v2/errors"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const streamStartedKey = "admin.stream.started"

var responseJSON = protojson.MarshalOptions{EmitUnpopulated: true}

func EncodeAPIResponse(c *app.RequestContext, value proto.Message) error {
	switch reply := value.(type) {
	case *apiV1.BinaryFileResponse:
		if !validResponseHeaders(reply.GetContentType(), reply.GetDisposition(), reply.GetCacheControl()) {
			return operatorV1.ErrorAdminBadRequest(errorFileResponseHeaders)
		}
		if reply.GetContentType() != "" {
			c.Response.Header.SetContentType(reply.GetContentType())
		}
		if reply.GetDisposition() != "" {
			c.Response.Header.Set("Content-Disposition", reply.GetDisposition())
		}
		if reply.GetCacheControl() != "" {
			c.Response.Header.Set("Cache-Control", reply.GetCacheControl())
		}
		status := int(reply.GetStatus())
		if status == 0 {
			status = http.StatusOK
		}
		c.SetStatusCode(status)
		c.Response.SetBody(reply.GetContent())
		return nil
	case *apiV1.DownloadFileResponse:
		if _, ok := reply.GetContent().(*apiV1.DownloadFileResponse_File); ok {
			return encodeDownload(c, reply)
		}
	}
	data, err := responseJSON.Marshal(value)
	if err != nil {
		return err
	}
	c.Data(http.StatusOK, "application/json", data)
	return nil
}
func encodeAPIError(c *app.RequestContext, err error) {
	failure := kerrors.FromError(err)
	body := &apiV1.HTTPErrorResponse{
		Code:     failure.GetCode(),
		Reason:   failure.GetReason(),
		Message:  failure.GetMessage(),
		Metadata: failure.GetMetadata(),
	}
	data, encodeErr := responseJSON.Marshal(body)
	if encodeErr != nil {
		slog.Error("encode HTTP error", "error", encodeErr)
		c.Data(http.StatusInternalServerError, "application/json", []byte(`{"code":500,"reason":"INTERNAL_SERVER_ERROR","message":"internal server error"}`))
		return
	}
	code := int(failure.GetCode())
	if code < 400 || code > 599 {
		code = http.StatusInternalServerError
	}
	c.Data(code, "application/json", data)
}

type byteRange struct{ start, end int }

func parseRanges(value string, size int) ([]byteRange, error) {
	if !strings.HasPrefix(value, "bytes=") {
		return nil, fmt.Errorf("invalid range")
	}
	ranges := make([]byteRange, 0)
	for _, part := range strings.Split(strings.TrimPrefix(value, "bytes="), ",") {
		pieces := strings.SplitN(strings.TrimSpace(part), "-", 2)
		if len(pieces) != 2 {
			return nil, fmt.Errorf("invalid range")
		}
		start, end := 0, size-1
		if pieces[0] == "" {
			suffix, err := strconv.Atoi(pieces[1])
			if err != nil || suffix <= 0 {
				return nil, fmt.Errorf("invalid suffix range")
			}
			if suffix < size {
				start = size - suffix
			}
		} else {
			parsed, err := strconv.Atoi(pieces[0])
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid range start")
			}
			start = parsed
			if pieces[1] != "" {
				parsed, err := strconv.Atoi(pieces[1])
				if err != nil || parsed < start {
					return nil, fmt.Errorf("invalid range end")
				}
				if parsed < end {
					end = parsed
				}
			}
		}
		if start >= size || size == 0 {
			continue
		}
		ranges = append(ranges, byteRange{
			start: start,
			end:   end,
		})
		if len(ranges) > 32 {
			return nil, fmt.Errorf("too many ranges")
		}
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("range is unsatisfiable")
	}
	return ranges, nil
}
func encodeDownload(c *app.RequestContext, reply *apiV1.DownloadFileResponse) error {
	contentType := reply.GetMime()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	disposition := c.Query("disposition")
	if disposition == "" {
		disposition = mime.FormatMediaType("attachment", map[string]string{"filename": reply.GetSourceFileName()})
	}
	if !validResponseHeaders(contentType, disposition) {
		return operatorV1.ErrorAdminBadRequest(errorFileResponseHeaders)
	}
	c.Response.Header.SetContentType(contentType)
	c.Response.Header.Set("Content-Disposition", disposition)
	c.Response.Header.Set("Accept-Ranges", "bytes")
	data := reply.GetFile()
	raw := string(c.Request.Header.Peek("Range"))
	if len(c.Request.Header.Peek("If-Range")) > 0 {
		raw = ""
	}
	if raw == "" {
		c.SetStatusCode(http.StatusOK)
		c.Response.SetBody(data)
		return nil
	}
	ranges, err := parseRanges(raw, len(data))
	if err != nil {
		c.Response.Header.Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
		c.Data(http.StatusRequestedRangeNotSatisfiable, "text/plain; charset=utf-8", []byte("invalid range\n"))
		return nil
	}
	total := 0
	for _, part := range ranges {
		total += part.end - part.start + 1
	}
	if total > len(data) {
		c.SetStatusCode(http.StatusOK)
		c.Response.SetBody(data)
		return nil
	}
	if len(ranges) == 1 {
		part := ranges[0]
		c.Response.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", part.start, part.end, len(data)))
		c.SetStatusCode(http.StatusPartialContent)
		c.Response.SetBody(data[part.start : part.end+1])
		return nil
	}
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	for _, part := range ranges {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", contentType)
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", part.start, part.end, len(data)))
		item, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err := item.Write(data[part.start : part.end+1]); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	c.Response.Header.SetContentType("multipart/byteranges; boundary=" + writer.Boundary())
	c.SetStatusCode(http.StatusPartialContent)
	c.Response.SetBody(output.Bytes())
	return nil
}

func validResponseHeaders(values ...string) bool {
	for _, value := range values {
		if strings.ContainsAny(value, "\r\n\x00") {
			return false
		}
	}
	return true
}
