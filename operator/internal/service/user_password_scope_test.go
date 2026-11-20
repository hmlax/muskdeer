package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func TestPlatformPasswordResetTargetsOneSameNameUser(t *testing.T) {
	for _, target := range []uint32{0, 1} {
		t.Run(map[uint32]string{0: "legacy platform credential", 1: "tenant credential"}[target], func(t *testing.T) {
			e := newUserServiceForTest(t)
			tenantA := e.seedUserSvcTenant(t, "Reset scope A", "RESET_SCOPE_A")
			tenantB := e.seedUserSvcTenant(t, "Reset scope B", "RESET_SCOPE_B")
			tenants := []*uint32{nil, trans.Ptr(tenantA), trans.Ptr(tenantB)}
			for i, tenantID := range tenants {
				userID := uint32(10700 + i)
				e.seedUserSvcCredential(t, tenantID, userID, "same-owner")
				e.stub.usersByID[userID] = &operatorV1.User{
					Id:       trans.Ptr(userID),
					TenantId: tenantID,
					Username: trans.Ptr("same-owner"),
				}
			}
			e.registerOperator(9001)
			_, err := e.svc.EditUserPassword(opCtx(e.ctx, 9001, 0, true), &operatorV1.EditUserPasswordRequest{
				UserId:      10700 + target,
				NewPassword: encryptLoginPassword(t, "ScopedP@ss77"),
			})
			require.NoError(t, err)
			for i, tenantID := range tenants {
				plain := constants.DefaultUserPassword
				if uint32(i) == target {
					plain = "ScopedP@ss77"
				}
				tid := uint32(0)
				if tenantID != nil {
					tid = *tenantID
				}
				_, err := e.findCred(t, tid, "same-owner", plain)
				require.NoError(t, err, "only the requested user/tenant may change password")
			}
		})
	}
}
