package oss

import (
	"errors"

	"github.com/minio/minio-go/v7"
)

var ErrInvalidDownloadRange = errors.New("invalid download range")

// SetDownloadRange converts the API's [start, end) interval to an HTTP byte range.
// An omitted/zero end is open-ended; 0/0 requests the whole object.
func SetDownloadRange(opts *minio.GetObjectOptions, start, end *int64) error {
	if opts == nil {
		return nil
	}
	var offset int64
	if start != nil {
		offset = *start
	}
	if offset < 0 || (end != nil && *end != 0 && *end <= offset) {
		return ErrInvalidDownloadRange
	}
	if end != nil && *end > 0 {
		return opts.SetRange(offset, *end-1)
	}
	if offset > 0 {
		return opts.SetRange(offset, 0)
	}
	return nil
}
