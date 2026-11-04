package coreentry

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCancellationClassificationNeverHidesJoinedFailure(t *testing.T) {
	failure := errors.New("cleanup failed")
	cases := []struct {
		err  error
		want bool
	}{
		{
			err:  nil,
			want: false,
		},
		{
			err:  context.Canceled,
			want: true,
		},
		{
			err:  fmt.Errorf("stopping: %w", context.Canceled),
			want: true,
		},
		{
			err:  errors.Join(context.Canceled, context.Canceled),
			want: true,
		},
		{
			err:  errors.Join(context.Canceled, failure),
			want: false,
		},
		{
			err:  context.DeadlineExceeded,
			want: false,
		},
	}
	for _, test := range cases {
		if got := IsCancellation(test.err); got != test.want {
			t.Fatalf("IsCancellation(%v)=%v, want %v", test.err, got, test.want)
		}
	}
}
