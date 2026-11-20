package data

import (
	"context"
	"time"

	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/membershiprole"
)

type MembershipRoleRepo struct {
	log             *bLogger.Helper
	dbClient        *store.DBClient
	statusConverter *mapper.EnumTypeConverter[operatorV1.MembershipRole_Status, membershiprole.Status]
}

func NewMembershipRoleRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *MembershipRoleRepo {
	return &MembershipRoleRepo{
		log:      ctx.NewLoggerHelper("membership-role/repo/admin-service"),
		dbClient: dbClient,
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.MembershipRole_Status, membershiprole.Status](
			operatorV1.MembershipRole_Status_name,
			operatorV1.MembershipRole_Status_value,
		),
	}
}

// CleanRelationsByMembershipID 删除会员的所有角色关联
func (r *MembershipRoleRepo) CleanRelationsByMembershipID(ctx context.Context, tx *store.Tx, membershipID uint32) error {
	if membershipID == 0 {
		return nil
	}

	if _, err := tx.MembershipRole.Delete().
		Where(
			membershiprole.MembershipIDEQ(membershipID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership roles failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old membership roles failed")
	}
	return nil
}

// CleanRelationsByMembershipIDs 删除多个会员的所有角色关联
func (r *MembershipRoleRepo) CleanRelationsByMembershipIDs(ctx context.Context, tx *store.Tx, membershipIDs []uint32) error {
	if len(membershipIDs) == 0 {
		return nil
	}

	if _, err := tx.MembershipRole.Delete().
		Where(
			membershiprole.MembershipIDIn(membershipIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership roles by membership ids failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old membership roles by membership ids failed")
	}
	return nil
}

// CleanRelationsByRoleID 删除角色的所有会员关联
func (r *MembershipRoleRepo) CleanRelationsByRoleID(ctx context.Context, tx *store.Tx, roleID uint32) error {
	if roleID == 0 {
		return nil
	}

	if _, err := tx.MembershipRole.Delete().
		Where(
			membershiprole.RoleIDEQ(roleID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership roles by role id failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old membership roles by role id failed")
	}
	return nil
}

// CleanRelationsByRoleIDs 删除多个角色的所有会员关联
func (r *MembershipRoleRepo) CleanRelationsByRoleIDs(ctx context.Context, tx *store.Tx, roleIDs []uint32) error {
	if len(roleIDs) == 0 {
		return nil
	}

	if _, err := tx.MembershipRole.Delete().
		Where(
			membershiprole.RoleIDIn(roleIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership roles by role ids failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old membership roles by role ids failed")
	}
	return nil
}

// RemoveRolesFromMembership 移除角色
func (r *MembershipRoleRepo) RemoveRolesFromMembership(ctx context.Context, membershipID uint32, roleIDs []uint32) error {
	if membershipID == 0 || len(roleIDs) == 0 {
		return nil
	}

	_, err := r.dbClient.Client().MembershipRole.Delete().
		Where(
			membershiprole.And(
				membershiprole.MembershipIDEQ(membershipID),
				membershiprole.RoleIDIn(roleIDs...),
			),
		).
		Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "remove roles from membership failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("remove roles from membership failed")
	}
	return nil
}

// AssignMembershipRoles 分配角色
func (r *MembershipRoleRepo) AssignMembershipRoles(ctx context.Context,
	tx *store.Tx,
	membershipID uint32,
	datas []*operatorV1.MembershipRole,
) error {
	if membershipID == 0 || len(datas) == 0 {
		return nil
	}

	var err error

	// 删除该用户的所有旧关联
	if err = r.CleanRelationsByMembershipID(ctx, tx, membershipID); err != nil {
		return operatorV1.ErrorPermissionInternalServerError("clean old membership roles failed")
	}

	now := time.Now()

	var membershipRoleCreates []*store.MembershipRoleCreate
	for _, data := range datas {
		if data.StartAt == nil {
			data.StartAt = timeutil.TimeToTimestamppb(&now)
		}

		rm := tx.MembershipRole.
			Create().
			SetNillableTenantID(data.TenantId).
			SetMembershipID(data.GetMembershipId()).
			SetRoleID(data.GetRoleId()).
			SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
			SetNillableAssignedBy(data.AssignedBy).
			SetNillableAssignedAt(timeutil.TimestamppbToTime(data.AssignedAt)).
			SetNillableIsPrimary(data.IsPrimary).
			SetNillableStartAt(timeutil.TimestamppbToTime(data.StartAt)).
			SetNillableEndAt(timeutil.TimestamppbToTime(data.EndAt)).
			SetCreatedAt(now).
			SetNillableCreatedBy(data.CreatedBy)
		membershipRoleCreates = append(membershipRoleCreates, rm)
	}

	_, err = tx.MembershipRole.CreateBulk(membershipRoleCreates...).Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "assign roles to membership failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("assign roles to membership failed")
	}

	return nil
}

// ListRoleIDs 获取用户关联的角色ID列表
func (r *MembershipRoleRepo) ListRoleIDs(ctx context.Context, membershipID uint32, excludeExpired bool) ([]uint32, error) {
	if membershipID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().MembershipRole.Query().
		Where(
			membershiprole.MembershipIDEQ(membershipID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiprole.Or(
				membershiprole.EndAtIsNil(),
				membershiprole.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiprole.FieldRoleID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query role ids by membership id failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query role ids by membership id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListMembershipIDs 获取角色关联的会员ID列表
func (r *MembershipRoleRepo) ListMembershipIDs(ctx context.Context, roleID uint32, excludeExpired bool) ([]uint32, error) {
	if roleID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().MembershipRole.Query().
		Where(
			membershiprole.RoleIDEQ(roleID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiprole.Or(
				membershiprole.EndAtIsNil(),
				membershiprole.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiprole.FieldMembershipID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query membership ids by role id failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query membership ids by role id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListMembershipIDsByRoleIDs 获取多个角色关联的会员ID列表
func (r *MembershipRoleRepo) ListMembershipIDsByRoleIDs(ctx context.Context, roleIDs []uint32, excludeExpired bool) ([]uint32, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}

	q := r.dbClient.Client().MembershipRole.Query().
		Where(
			membershiprole.RoleIDIn(roleIDs...),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiprole.Or(
				membershiprole.EndAtIsNil(),
				membershiprole.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiprole.FieldMembershipID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query membership ids by role ids failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query membership ids by role ids failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}
