package store

import (
	"context"
	"errors"
	"testing"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	"gorm.io/gorm"
)

func TestNativeScopesRetainTenantBoundaryAndLiteralSearch(t *testing.T) {
	client := fixture(t)
	system := viewer.WithSystemContext(context.Background())
	for _, tenant := range []uint32{7, 8} {
		_, err := client.User.Create().SetTenantID(tenant).SetUsername("Report_100%Done").Save(system)
		require.NoError(t, err)
	}
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(1, 7, 0, "", nil))
	records, err := client.User.Query().Where(queryscope.Or(queryscope.ContainsFold("username", "REPORT_100%"), queryscope.Equal("tenant_id", 8))).All(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, uint32(7), *records[0].TenantID)
	count, err := client.User.Query().Where(queryscope.Contains("username", "Report_100%no-match")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = client.User.Query().Where(queryscope.In("id")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = client.User.Query().Where(queryscope.NotIn("id")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = client.User.Query().Where(func(db *gorm.DB) *gorm.DB { return db.Limit(1).Offset(1).Select("username") }).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "count must ignore paging and projection scopes")
	sentinel := errors.New("invalid association")
	_, err = client.User.Query().Where(queryscope.Or(func(db *gorm.DB) *gorm.DB { _ = db.AddError(sentinel); return db }, queryscope.Equal("tenant_id", 8))).All(ctx)
	require.ErrorIs(t, err, sentinel)
	_, err = client.User.Query().Select("id, (SELECT 1)").All(ctx)
	require.Error(t, err)
}

func TestNativeJSONPatchPreservesOtherFieldsAndZeroValues(t *testing.T) {
	client := fixture(t)
	ctx := viewer.WithSystemContext(context.Background())
	record, err := client.Menu.Create().SetName("native-json").SetMeta(&operatorV1.MenuMeta{
		Title:      trans.Ptr("unchanged"),
		Order:      trans.Ptr(int32(5)),
		HideInMenu: trans.Ptr(true),
		Icon:       trans.Ptr("before"),
	}).Save(ctx)
	require.NoError(t, err)
	err = client.Menu.UpdateOneID(record.ID).Modify(PatchJSON("meta",
		JSONField{
			Name:  "order",
			Value: int32(0),
		},
		JSONField{
			Name:  "hideInMenu",
			Value: false,
		},
		JSONField{
			Name:  "icon",
			Value: nil,
		},
	)).Exec(ctx)
	require.NoError(t, err)
	loaded, err := client.Menu.Get(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, "unchanged", loaded.Meta.GetTitle())
	require.NotNil(t, loaded.Meta.Order)
	require.Zero(t, loaded.Meta.GetOrder())
	require.NotNil(t, loaded.Meta.HideInMenu)
	require.False(t, loaded.Meta.GetHideInMenu())
	require.Nil(t, loaded.Meta.Icon)
}
