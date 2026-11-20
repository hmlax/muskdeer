package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func bindingContext(ctx context.Context, userID, tenantID uint32) context.Context {
	ctx = viewer.WithContext(ctx, viewer.NewUserContext(uint64(userID), uint64(tenantID), 0, "", nil))
	return auth.NewContext(ctx, &pb.UserTokenPayload{
		UserId:   userID,
		TenantId: &tenantID,
	})
}

func TestMuskdeerBindingScopesRevocationAndPermanentOwnership(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	for _, id := range []uint32{7, 8} {
		_, err := db.Client().Tenant.Create().SetID(id).SetName("merchant" + string(rune('a'+id))).SetCode(string(rune('a' + id))).Save(system)
		require.NoError(t, err)
	}
	repo := NewMuskdeerBindingRepo(db)
	platform := bindingContext(t.Context(), 1, 0)
	merchant := bindingContext(t.Context(), 2, 7)
	other := bindingContext(t.Context(), 3, 8)
	_, err := repo.Scope(platform)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "platform can never become an unrestricted trading scope")
	_, err = repo.Scope(merchant)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	row, err := repo.Create(platform, &pb.MuskdeerBindingDraft{
		TenantId:  7,
		AccountId: "venue/100/100",
		Enabled:   true,
	})
	require.NoError(t, err)
	scope, err := repo.Scope(merchant)
	require.NoError(t, err)
	require.Equal(t, []string{row.AccountId}, scope)
	_, err = repo.Scope(other)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = repo.List(merchant, &pb.MuskdeerBindingListRequest{TenantId: 8})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = repo.SetEnabled(merchant, &pb.MuskdeerSetBindingEnabledRequest{
		Id:              row.Id,
		ExpectedVersion: 1,
		Enabled:         false,
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	disabled, err := repo.SetEnabled(platform, &pb.MuskdeerSetBindingEnabledRequest{
		Id:              row.Id,
		ExpectedVersion: 1,
		Enabled:         false,
	})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Equal(t, uint64(2), disabled.Version)
	_, err = repo.Scope(merchant)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = repo.SetEnabled(platform, &pb.MuskdeerSetBindingEnabledRequest{
		Id:              row.Id,
		ExpectedVersion: 1,
		Enabled:         true,
	})
	require.Equal(t, codes.Aborted, status.Code(err))
	_, err = repo.Create(platform, &pb.MuskdeerBindingDraft{
		TenantId:  8,
		AccountId: row.AccountId,
		Enabled:   true,
	})
	require.Error(t, err, "disabled account ownership must not transfer")
	_, err = repo.Scope(t.Context())
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}
