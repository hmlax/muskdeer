package oss

import (
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

// TestSetDownloadRange 表驱动校验区间写入：
// API 的 [start,end) 转为 HTTP 的闭区间；仅 start 表示后缀；0/0 为全量。
// 双无 → 不设置头；nil opts → 直接返回不 panic。
func TestSetDownloadRange(t *testing.T) {
	tests := []struct {
		name      string
		start     *int64
		end       *int64
		wantRange string
	}{
		{"both start and end", int64Ptr(5), int64Ptr(10), "bytes=5-9"},
		{"start only", int64Ptr(5), nil, "bytes=5-"},
		{"end only positive", nil, int64Ptr(5), "bytes=0-4"},
		{"first byte", int64Ptr(0), int64Ptr(1), "bytes=0-0"},
		{"zero means full object", int64Ptr(0), int64Ptr(0), ""},
		{"neither", nil, nil, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var opts minio.GetObjectOptions
			require.NoError(t, SetDownloadRange(&opts, tt.start, tt.end))
			require.Equal(t, tt.wantRange, opts.Header().Get("Range"),
				"Range header for start=%v end=%v", tt.start, tt.end)
		})
	}
}

func TestSetDownloadRangeRejectsInvalidIntervals(t *testing.T) {
	for _, bounds := range [][2]int64{{-1, 5}, {1, -5}, {5, 5}, {10, 5}} {
		var opts minio.GetObjectOptions
		require.ErrorIs(t, SetDownloadRange(&opts, &bounds[0], &bounds[1]), ErrInvalidDownloadRange)
		require.Empty(t, opts.Header().Get("Range"))
	}
}

// TestSetDownloadRange_NilOpts nil 选项对象必须直接返回且不 panic。
func TestSetDownloadRange_NilOpts(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		_ = SetDownloadRange(nil, nil, nil)
	})
}

// int64Ptr 构造 int64 指针的测试辅助。
func int64Ptr(v int64) *int64 { return &v }
