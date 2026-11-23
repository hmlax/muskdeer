package service

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
)

func TestMuskdeerPlatformCannotReachMerchantBusinessEvenWithGlobalAdminPermission(t *testing.T) {
	s := &MuskdeerService{log: bLogger.NewHelper(bLogger.NopLogger())}
	ctx := auth.NewContext(viewer.WithSystemContext(t.Context()), &pb.UserTokenPayload{UserId: 1})
	checks := []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "maker-check",
			call: func(ctx context.Context) error {
				_, err := s.CheckInstance(ctx, &pb.MuskdeerInstanceRequest{})
				return err
			},
		},
		{
			name: "enable-account",
			call: func(ctx context.Context) error {
				_, err := s.EnableAccount(ctx, &pb.MuskdeerEnableAccountRequest{})
				return err
			},
		},
		{
			name: "overview",
			call: func(ctx context.Context) error {
				_, err := s.GetOverview(ctx, &pb.MuskdeerOverviewRequest{})
				return err
			},
		},
		{
			name: "accounts",
			call: func(ctx context.Context) error { _, err := s.ListAccounts(ctx, &pb.MuskdeerListRequest{}); return err },
		},
		{
			name: "create-account",
			call: func(ctx context.Context) error {
				_, err := s.CreateAccount(ctx, &pb.MuskdeerCreateAccountRequest{})
				return err
			},
		},
		{
			name: "instances",
			call: func(ctx context.Context) error {
				_, err := s.ListInstances(ctx, &pb.MuskdeerInstanceListRequest{})
				return err
			},
		},
		{
			name: "instance",
			call: func(ctx context.Context) error {
				_, err := s.GetInstance(ctx, &pb.MuskdeerInstanceRequest{})
				return err
			},
		},
		{
			name: "create",
			call: func(ctx context.Context) error {
				_, err := s.CreateInstance(ctx, &pb.MuskdeerCreateInstanceRequest{})
				return err
			},
		},
		{
			name: "update",
			call: func(ctx context.Context) error {
				_, err := s.UpdateInstance(ctx, &pb.MuskdeerUpdateInstanceRequest{})
				return err
			},
		},
		{
			name: "start",
			call: func(ctx context.Context) error {
				_, err := s.StartInstance(ctx, &pb.MuskdeerRunningRequest{})
				return err
			},
		},
		{
			name: "stop",
			call: func(ctx context.Context) error {
				_, err := s.StopInstance(ctx, &pb.MuskdeerRunningRequest{})
				return err
			},
		},
		{
			name: "cancel",
			call: func(ctx context.Context) error {
				_, err := s.CancelInstanceOrders(ctx, &pb.MuskdeerCancelRequest{})
				return err
			},
		},
		{
			name: "snapshot",
			call: func(ctx context.Context) error {
				_, err := s.GetAccountSnapshot(ctx, &pb.MuskdeerAccountRequest{})
				return err
			},
		},
		{
			name: "orders",
			call: func(ctx context.Context) error {
				_, err := s.ListOrderRecords(ctx, &pb.MuskdeerRecordListRequest{})
				return err
			},
		},
		{
			name: "cancellations",
			call: func(ctx context.Context) error {
				_, err := s.ListCancellations(ctx, &pb.MuskdeerRecordListRequest{})
				return err
			},
		},
		{
			name: "synthetic",
			call: func(ctx context.Context) error {
				_, err := s.ListSyntheticRecords(ctx, &pb.MuskdeerRecordListRequest{})
				return err
			},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			require.Equal(t, 403, kratoserrors.Code(check.call(ctx)), "must reject before binding lookup or core RPC")
		})
	}
}
