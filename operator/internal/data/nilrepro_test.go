package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

// 复现 Update 带 role_ids（mask 含 role_ids）时的空指针 panic。
func TestReproNilPanicOnRoleIdsUpdate(t *testing.T) {
	r := newUserRepoSqlite(t)
	ctx := storetest.NewSystemContext(context.Background())

	created, err := r.Create(ctx, &operatorV1.CreateUserRequest{
		Data: &operatorV1.User{
			Username: trans.Ptr("nilpanic_u"),
			Status:   operatorV1.User_NORMAL.Enum(),
		},
	})
	require.NoError(t, err)

	req := &operatorV1.UpdateUserRequest{
		Id:         *created.Id,
		Data:       &operatorV1.User{RoleIds: []uint32{1}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"role_ids"}},
	}
	require.NotPanics(t, func() {
		err = r.Update(ctx, req)
	})
	require.NoError(t, err)
}
