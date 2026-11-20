package service

import (
	"slices"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func updatesField(mask *fieldmaskpb.FieldMask, names ...string) bool {
	if mask == nil || len(mask.Paths) == 0 {
		return true
	}
	for _, path := range mask.Paths {
		if slices.Contains(names, path) {
			return true
		}
	}
	return false
}

func userAuthorizationChanged(previous *operatorV1.User, req *operatorV1.UpdateUserRequest) bool {
	if updatesField(req.UpdateMask, "status") && req.Data.Status != nil && previous.GetStatus() != req.Data.GetStatus() {
		return true
	}
	if !updatesField(req.UpdateMask, "role_ids", "roleIds", "role_id", "roleId") {
		return false
	}
	oldRoles := slices.Clone(previous.GetRoleIds())
	newRoles := slices.Clone(req.Data.GetRoleIds())
	slices.Sort(oldRoles)
	slices.Sort(newRoles)
	return !slices.Equal(slices.Compact(oldRoles), slices.Compact(newRoles))
}

func roleChangesTokenClaims(req *operatorV1.UpdateRoleRequest) bool {
	if req.UpdateMask == nil {
		return req.Data.DataScope != nil
	}
	return updatesField(req.UpdateMask, "field_permissions", "fieldPermissions", "data_scope", "dataScope", "org_units", "orgUnits")
}
