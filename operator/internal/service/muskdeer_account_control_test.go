package service

import (
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestDisabledAccountPreconditionRetainsSafePublicReason(t *testing.T) {
	server := &MuskdeerService{log: bLogger.NewHelper(bLogger.NopLogger())}
	failure, err := status.New(codes.FailedPrecondition, "internal details must not escape").WithDetails(&errdetails.ErrorInfo{Reason: domain.AccountDisabledReason})
	require.NoError(t, err)
	result := kratoserrors.FromError(server.failure(t.Context(), failure.Err()))
	require.Equal(t, int32(409), result.Code)
	require.Equal(t, domain.AccountDisabledReason, result.Reason)
	require.Equal(t, domain.AccountDisabledMessage, result.Message)
	unknown, err := status.New(codes.FailedPrecondition, "private connection failure").WithDetails(&errdetails.ErrorInfo{Reason: "UNRECOGNIZED"})
	require.NoError(t, err)
	other := kratoserrors.FromError(server.failure(t.Context(), unknown.Err()))
	require.Equal(t, muskdeerErrorReason, other.Reason)
	require.NotContains(t, other.Message, "private")
}
