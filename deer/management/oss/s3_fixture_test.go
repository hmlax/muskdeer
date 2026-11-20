package oss

import (
	"crypto/md5" // S3 ETags identify fixture bytes, not authentication.
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The wrapper tests use the actual MinIO HTTP client against an owned S3 fixture.
// They never discover, seed or modify a developer's object storage service.
type fixtureObject struct {
	body []byte
	mime string
}

type s3Fixture struct {
	mu      sync.Mutex
	buckets map[string]map[string]fixtureObject
}

func newS3Fixture() *httptest.Server {
	fixture := &s3Fixture{buckets: make(map[string]map[string]fixtureObject)}
	return httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
}

func fixtureError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>fixture request failed</Message></Error>", code)
}

func (f *s3Fixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, object, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	items, exists := f.buckets[bucket]
	if _, location := r.URL.Query()["location"]; location {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	if object == "" {
		f.bucketRequest(w, r, bucket, items, exists)
		return
	}
	if !exists {
		fixtureError(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var reader io.Reader = r.Body
		if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
			reader = httputil.NewChunkedReader(reader)
		}
		if size, err := strconv.ParseInt(r.Header.Get("X-Amz-Decoded-Content-Length"), 10, 64); err == nil {
			reader = io.LimitReader(reader, size)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			fixtureError(w, http.StatusBadRequest, "InvalidRequest")
			return
		}
		items[object] = fixtureObject{
			body: body,
			mime: r.Header.Get("Content-Type"),
		}
		etag := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(etag[:])+`"`)
	case http.MethodDelete:
		delete(items, object)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodHead, http.MethodGet:
		value, found := items[object]
		if !found {
			fixtureError(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		serveFixtureObject(w, r, value)
	default:
		fixtureError(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (f *s3Fixture) bucketRequest(w http.ResponseWriter, r *http.Request, bucket string, items map[string]fixtureObject, exists bool) {
	switch r.Method {
	case http.MethodHead:
		if !exists {
			fixtureError(w, http.StatusNotFound, "NoSuchBucket")
		}
	case http.MethodPut:
		if exists {
			fixtureError(w, http.StatusConflict, "BucketAlreadyOwnedByYou")
			return
		}
		f.buckets[bucket] = make(map[string]fixtureObject)
	case http.MethodDelete:
		delete(f.buckets, bucket)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		if !exists {
			fixtureError(w, http.StatusNotFound, "NoSuchBucket")
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated>`)
		for key := range items {
			if strings.HasPrefix(key, r.URL.Query().Get("prefix")) {
				_, _ = io.WriteString(w, "<Contents><Key>")
				_ = xml.EscapeText(w, []byte(key))
				_, _ = io.WriteString(w, "</Key></Contents>")
			}
		}
		_, _ = io.WriteString(w, "</ListBucketResult>")
	default:
		fixtureError(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveFixtureObject(w http.ResponseWriter, r *http.Request, value fixtureObject) {
	content := value.body
	w.Header().Set("Content-Type", value.mime)
	w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", `"fixture-etag"`)
	status := http.StatusOK
	if interval := strings.TrimPrefix(r.Header.Get("Range"), "bytes="); interval != "" {
		left, right, _ := strings.Cut(interval, "-")
		start, _ := strconv.Atoi(left)
		end := len(content) - 1
		if right != "" {
			end, _ = strconv.Atoi(right)
		}
		if start < 0 || end >= len(content) || start > end {
			fixtureError(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange")
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		content = content[start : end+1]
		status = http.StatusPartialContent
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(status)
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
}
