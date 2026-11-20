package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func issueSessionForTest(t *testing.T, authenticator *data.Authenticator, userID uint32) string {
	t.Helper()
	token, _, err := authenticator.CreateUserToken(t.Context(), operatorV1.ClientType_admin, &operatorV1.UserTokenPayload{UserId: userID})
	require.NoError(t, err)
	assertSessionForTest(t, authenticator, token, true)
	return token
}

func assertSessionForTest(t *testing.T, authenticator *data.Authenticator, token string, valid bool) {
	t.Helper()
	result, err := authenticator.Authenticate(t.Context(), &operatorV1.ValidateTokenRequest{
		Token:         token,
		TokenCategory: operatorV1.TokenCategory_ACCESS,
		ClientType:    operatorV1.ClientType_admin,
	})
	if valid {
		require.NoError(t, err)
		require.True(t, result.GetIsValid())
		return
	}
	require.False(t, result.GetIsValid(), "a revoked session must not authenticate")
}

func TestUserAuthorizationUpdatesRevokeSessions(t *testing.T) {
	for _, test := range []struct {
		name       string
		changeRole bool
		status     operatorV1.User_Status
		paths      []string
		revoked    bool
	}{
		{
			name:       "role changed",
			changeRole: true,
			status:     operatorV1.User_NORMAL,
			paths:      []string{"roleIds"},
			revoked:    true,
		},
		{
			name:    "user disabled",
			status:  operatorV1.User_DISABLED,
			paths:   []string{"status"},
			revoked: true,
		},
		{
			name:   "same authorization",
			status: operatorV1.User_NORMAL,
			paths:  []string{"role_ids", "status"},
		},
		{
			name:       "roles outside mask",
			changeRole: true,
			status:     operatorV1.User_NORMAL,
			paths:      []string{"nickname"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newUserServiceForTest(t)
			oldRole := e.seedUserSvcRole(t, nil, operatorV1.Role_SYSTEM, "session_old")
			newRole := e.seedUserSvcRole(t, nil, operatorV1.Role_SYSTEM, "session_new")
			e.registerOperator(9001)
			const userID uint32 = 10901
			e.stub.usersByID[userID] = &operatorV1.User{
				Id:       trans.Ptr(userID),
				Username: trans.Ptr("session-member"),
				RoleIds:  []uint32{oldRole},
				Status:   operatorV1.User_NORMAL.Enum(),
			}
			roleID := oldRole
			if test.changeRole {
				roleID = newRole
			}
			token := issueSessionForTest(t, e.svc.authenticator, userID)
			_, err := e.svc.Update(opCtx(e.ctx, 9001, 0, true), &operatorV1.UpdateUserRequest{
				Id: userID,
				Data: &operatorV1.User{
					RoleIds: []uint32{roleID},
					Status:  test.status.Enum(),
				},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: test.paths},
			})
			require.NoError(t, err)
			assertSessionForTest(t, e.svc.authenticator, token, !test.revoked)
		})
	}
}

func TestDeletingUserRevokesSessions(t *testing.T) {
	e := newUserServiceForTest(t)
	e.registerOperator(9001)
	const userID uint32 = 10902
	e.stub.usersByID[userID] = &operatorV1.User{
		Id:       trans.Ptr(userID),
		Username: trans.Ptr("deleted-session-member"),
	}
	token := issueSessionForTest(t, e.svc.authenticator, userID)
	_, err := e.svc.Delete(opCtx(e.ctx, 9001, 0, true), &operatorV1.DeleteUserRequest{
		QueryBy: &operatorV1.DeleteUserRequest_Id{Id: userID},
	})
	require.NoError(t, err)
	assertSessionForTest(t, e.svc.authenticator, token, false)
}

func TestDeletingUserByUsernameUsesResolvedIdentity(t *testing.T) {
	e := newUserServiceForTest(t)
	e.registerOperator(9001)
	const userID uint32 = 10903
	const username = "deleted-by-username"
	e.stub.usersByID[userID] = &operatorV1.User{
		Id:       trans.Ptr(userID),
		Username: trans.Ptr(username),
	}
	token := issueSessionForTest(t, e.svc.authenticator, userID)
	request := &operatorV1.DeleteUserRequest{
		QueryBy: &operatorV1.DeleteUserRequest_Username{Username: username},
	}
	_, err := e.svc.Delete(opCtx(e.ctx, 9001, 0, true), request)
	require.NoError(t, err)
	require.Equal(t, []uint32{userID}, e.stub.deletes)
	require.Equal(t, username, request.GetUsername())
	assertSessionForTest(t, e.svc.authenticator, token, false)
}

func TestDeletingUserByUsernamePreservesProtection(t *testing.T) {
	for _, test := range []struct {
		name     string
		id       uint32
		username string
	}{
		{
			name:     "default admin",
			id:       1,
			username: constants.DefaultAdminUserName,
		},
		{
			name:     "self",
			id:       9001,
			username: "session-self",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newUserServiceForTest(t)
			e.registerOperator(9001)
			e.stub.usersByID[test.id] = &operatorV1.User{
				Id:       trans.Ptr(test.id),
				Username: trans.Ptr(test.username),
			}
			token := issueSessionForTest(t, e.svc.authenticator, test.id)
			_, err := e.svc.Delete(opCtx(e.ctx, 9001, 0, true), &operatorV1.DeleteUserRequest{
				QueryBy: &operatorV1.DeleteUserRequest_Username{Username: test.username},
			})
			require.True(t, operatorV1.IsAdminBadRequest(err), "protected user must remain protected through the username alias: %v", err)
			require.Empty(t, e.stub.deletes)
			assertSessionForTest(t, e.svc.authenticator, token, true)
		})
	}
}

func TestRoleClaimChangesRevokeSessions(t *testing.T) {
	for _, action := range []string{"field permissions", "data scope", "role disabled", "delete"} {
		t.Run(action, func(t *testing.T) {
			svc, client := newRoleServiceFixture(t)
			ctx := opCtx(storetest.NewSystemContext(t.Context()), 9001, 0, true)
			row, err := client.Client().Role.Create().SetName("session-role").SetCode("session-role").Save(ctx)
			require.NoError(t, err)
			member, err := client.Client().User.Create().SetUsername("role-session-member").Save(ctx)
			require.NoError(t, err)
			require.NoError(t, client.Client().UserRole.Create().SetRoleID(row.ID).SetUserID(member.ID).Exec(ctx))
			token := issueSessionForTest(t, svc.authenticator, member.ID)
			switch action {
			case "delete":
				_, err = svc.Delete(ctx, &operatorV1.DeleteRoleRequest{
					QueryBy: &operatorV1.DeleteRoleRequest_Id{Id: row.ID},
				})
			case "field permissions":
				_, err = svc.Update(ctx, &operatorV1.UpdateRoleRequest{
					Id: row.ID,
					Data: &operatorV1.Role{FieldPermissions: []*operatorV1.RoleFieldPermission{{
						Resource:     "User",
						HiddenFields: []string{"email"},
					}}},
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"fieldPermissions"}},
				})
			case "data scope":
				_, err = svc.Update(ctx, &operatorV1.UpdateRoleRequest{
					Id:         row.ID,
					Data:       &operatorV1.Role{DataScope: operatorV1.DataScope_SELF.Enum()},
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dataScope"}},
				})
			case "role disabled":
				_, err = svc.Update(ctx, &operatorV1.UpdateRoleRequest{
					Id:         row.ID,
					Data:       &operatorV1.Role{Status: operatorV1.Role_OFF.Enum()},
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
				})
			}
			require.NoError(t, err)
			assertSessionForTest(t, svc.authenticator, token, false)
		})
	}
}
