package bootstrap

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tx7do/go-utils/trans"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func contractRouter(t *testing.T) *Router {
	t.Helper()
	router, err := newAPIRouter(&conf.Bootstrap{})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

type contractFiles struct {
	apiV1.FileTransferServiceHTTPServer
	upload   *apiV1.UploadFileRequest
	download *apiV1.DownloadFileRequest
	content  []byte
}

func (s *contractFiles) PostUploadFile(_ context.Context, in *apiV1.UploadFileRequest) (*apiV1.UploadFileResponse, error) {
	s.upload = in
	return &apiV1.UploadFileResponse{ObjectName: trans.Ptr("stored")}, nil
}
func (s *contractFiles) DownloadFile(_ context.Context, in *apiV1.DownloadFileRequest) (*apiV1.DownloadFileResponse, error) {
	s.download = in
	return &apiV1.DownloadFileResponse{
		Content:        &apiV1.DownloadFileResponse_File{File: s.content},
		SourceFileName: "test.txt",
		Mime:           "text/plain",
	}, nil
}

func TestIDLFileBindingsPreserveMultipartAndRange(t *testing.T) {
	router := contractRouter(t)
	files := &contractFiles{content: []byte("abcdefgh")}
	RegisterFileTransferServiceHertzServer(router, files)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("mime", "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/file/upload", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != 200 || files.upload.GetSourceFileName() != "upload.txt" || string(files.upload.GetFile()) != "hello" || files.upload.GetSize() != 5 || files.upload.GetMime() != "text/plain" {
		t.Fatalf("multipart binding = %d %s, upload=%v", response.Code, response.Body.String(), files.upload)
	}
	request = httptest.NewRequest(http.MethodGet, "/admin/v1/file/download?fileId=4", nil)
	request.Header.Set("Range", "bytes=2-4")
	response = httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != 206 || response.Body.String() != "cde" || response.Header().Get("Content-Range") != "bytes 2-4/8" || files.download.GetFileId() != 4 {
		t.Fatalf("range binding = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
	// An explicitly selected empty file must remain a binary response.
	files.content = []byte{}
	response = httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/admin/v1/file/download?fileId=4", nil))
	if response.Code != 200 || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("empty file = %d %s", response.Code, response.Body.String())
	}
}

type contractExports struct {
	operatorV1.ExportServiceClient
	request *operatorV1.AuditExportRequest
}

func (c *contractExports) ExportAuditLogs(_ context.Context, in *operatorV1.AuditExportRequest, _ ...grpc.CallOption) (*operatorV1.BinaryFileResponse, error) {
	c.request = in
	return &operatorV1.BinaryFileResponse{
		Content:     []byte("name,count\nhello,2\n"),
		ContentType: "text/csv",
		Disposition: `attachment; filename="audit.csv"`,
	}, nil
}

func TestIDLExportBindingsUseTypedQueriesAndBinaryReplies(t *testing.T) {
	router := contractRouter(t)
	client := &contractExports{}
	RegisterExportServiceHertzServer(router, newExportHTTPService(client))
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/audit-logs:export?format=csv&maxRows=23&query=hello", nil)
	request = request.WithContext(auth.NewContext(request.Context(), &operatorV1.UserTokenPayload{
		UserId:          42,
		IsPlatformAdmin: trans.Ptr(true),
	}))
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != 200 || response.Body.String() != "name,count\nhello,2\n" || response.Header().Get("Content-Type") != "text/csv" || !strings.Contains(response.Header().Get("Content-Disposition"), "audit.csv") {
		t.Fatalf("export reply = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
	if in := client.request; in.GetFormat() != "csv" || in.GetMaxRows() != 23 || in.GetQuery() != "hello" {
		t.Fatalf("typed export query = %v", in)
	}
	response = httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/admin/v1/audit-logs:export?maxRows=invalid", nil))
	var failure apiV1.HTTPErrorResponse
	if err := protojson.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if response.Code != 400 || failure.GetCode() != 400 || failure.GetReason() == "" {
		t.Fatalf("typed binding error = %d %v", response.Code, &failure)
	}
}
