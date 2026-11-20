package oss

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
)

// These credentials belong only to the in-process HTTP protocol fixture.
var fixtureEndpoint string

func minioTestEndpoint() string  { return fixtureEndpoint }
func minioTestAccessKey() string { return "test-access-key" }
func minioTestSecretKey() string { return "test-secret-key" }

// createTestClient 构造指向测试端点（环境变量可覆盖）的 MinIOClient。
func createTestClient() *MinIOClient {
	endpoint := minioTestEndpoint()
	return NewMinIoClient(&conf.Bootstrap{
		Oss: &conf.OSS{
			Minio: &conf.OSS_MinIO{
				Endpoint:     endpoint,
				UploadHost:   endpoint,
				DownloadHost: endpoint,
				AccessKey:    minioTestAccessKey(),
				SecretKey:    minioTestSecretKey(),
			},
		},
	}, bLogger.NopLogger())
}

// minioAvailable 探测测试端点上的 MinIO 是否可达：环境依赖测试依赖真实 MinIO 服务，
// 无环境时 skip 而非失败——环境依赖测试不应拖红常规回归。
func minioAvailable() bool {
	conn, err := net.DialTimeout("tcp", minioTestEndpoint(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestMinIoClient(t *testing.T) {
	if !minioAvailable() {
		t.Fatal("owned S3 protocol fixture is unavailable")
	}
	cli := createTestClient()
	assert.NotNil(t, cli)

	resp, err := cli.GetUploadPresignedUrl(t.Context(), &operatorV1.GetUploadPresignedUrlRequest{
		Method:        operatorV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.String("image/jpeg"),
		BucketName:    trans.String("images"),
		FileDirectory: trans.String("20221010"),
	})
	assert.Nil(t, err)
	assert.NotNil(t, resp)
}

func TestListFile(t *testing.T) {
	cli := createTestClient()
	assert.NotNil(t, cli)

	req := &operatorV1.ListOssFileRequest{
		BucketName: trans.Ptr("users"),
		Folder:     trans.Ptr("1"),
		Recursive:  trans.Ptr(true),
	}
	files, err := cli.ListFile(t.Context(), req)
	assert.Nil(t, err)
	fmt.Println(files)
}

func TestDownloadFile(t *testing.T) {
	if !minioAvailable() {
		t.Fatal("owned S3 protocol fixture is unavailable")
	}
	cli := createTestClient()
	assert.NotNil(t, cli)

	resp, err := cli.DownloadFile(t.Context(), &operatorV1.DownloadFileRequest{
		Selector: &operatorV1.DownloadFileRequest_StorageObject{
			StorageObject: &operatorV1.StorageObject{
				BucketName: trans.Ptr("images"),
				ObjectName: trans.Ptr("DateTimePicker.png"),
			},
		},
		PreferPresignedUrl: trans.Ptr(false),
	})
	if err != nil {
		t.Error(err)
	}
	fmt.Println(resp)
}
