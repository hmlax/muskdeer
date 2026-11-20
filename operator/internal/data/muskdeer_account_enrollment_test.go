package data

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func TestMuskdeerSDKUserCannotBeBoundAcrossMerchants(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	_, err := db.Client().PermissionGroup.Create().SetName("market").SetModule(muskdeer.Module).Save(system)
	require.NoError(t, err)
	for _, id := range []uint32{7, 8} {
		_, err := db.Client().Tenant.Create().SetID(id).SetName(string(rune('a' + id))).SetCode(string(rune('a' + id))).Save(system)
		require.NoError(t, err)
	}
	repo := NewMuskdeerBindingRepo(db)
	platform := bindingContext(t.Context(), 1, 0)
	_, err = repo.Create(platform, &pb.MuskdeerBindingDraft{
		TenantId:  7,
		AccountId: "himalaya/100/100",
		Enabled:   true,
	})
	require.NoError(t, err)
	_, err = repo.Create(platform, &pb.MuskdeerBindingDraft{
		TenantId:  8,
		AccountId: "himalaya/100/101",
		Enabled:   true,
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err), "one SDK user cannot cross merchant ownership")
}

func TestMuskdeerEnrollmentKeepsPendingPrivateAndHonorsRevocation(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	_, err := db.Client().PermissionGroup.Create().SetName("market").SetModule(muskdeer.Module).Save(system)
	require.NoError(t, err)
	for _, id := range []uint32{7, 8} {
		_, err := db.Client().Tenant.Create().SetID(id).SetName(string(rune('a' + id))).SetCode(string(rune('a' + id))).Save(system)
		require.NoError(t, err)
	}
	repo := NewMuskdeerBindingRepo(db)
	platform := bindingContext(t.Context(), 1, 0)
	merchant := bindingContext(t.Context(), 2, 7)
	other := bindingContext(t.Context(), 3, 8)
	parent, err := repo.Create(platform, &pb.MuskdeerBindingDraft{
		TenantId:  7,
		AccountId: muskdeer.AccountScope(100, 100),
		Enabled:   true,
	})
	require.NoError(t, err)
	_, err = repo.BeginAccountEnrollment(merchant, 101, 102)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = repo.BeginAccountEnrollment(other, 100, 102)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	pending, err := repo.BeginAccountEnrollment(merchant, 100, 101)
	require.NoError(t, err)
	require.False(t, pending.Enabled)
	require.Equal(t, muskdeerBindingPendingVersion, pending.Version)
	scope, err := repo.Scope(merchant)
	require.NoError(t, err)
	require.Equal(t, []string{parent.AccountId}, scope)
	retry, err := repo.BeginAccountEnrollment(merchant, 100, 101)
	require.NoError(t, err)
	require.Equal(t, pending.Id, retry.Id, "timeout retries retain the original ownership reservation")
	require.Error(t, repo.CompleteAccountEnrollment(other, pending.Id))
	require.NoError(t, repo.CompleteAccountEnrollment(merchant, pending.Id))
	require.NoError(t, repo.CompleteAccountEnrollment(merchant, pending.Id))
	scope, err = repo.Scope(merchant)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{parent.AccountId, pending.AccountId}, scope)
	_, err = repo.SetEnabled(platform, &pb.MuskdeerSetBindingEnabledRequest{
		Id:              pending.Id,
		ExpectedVersion: muskdeerBindingActiveVersion,
		Enabled:         false,
	})
	require.NoError(t, err)
	require.Equal(t, codes.Aborted, status.Code(repo.CompleteAccountEnrollment(merchant, pending.Id)))
	_, err = repo.BeginAccountEnrollment(merchant, 100, 101)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	scope, err = repo.Scope(merchant)
	require.NoError(t, err)
	require.Equal(t, []string{parent.AccountId}, scope)
}
