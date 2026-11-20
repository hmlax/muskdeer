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
	"github.com/hmlax/muskdeer/operator/internal/data/store/membershiporgunit"
)

type MembershipOrgUnitRepo struct {
	log *bLogger.Helper

	dbClient        *store.DBClient
	statusConverter *mapper.EnumTypeConverter[operatorV1.MembershipOrgUnit_Status, membershiporgunit.Status]
}

func NewMembershipOrgUnitRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *MembershipOrgUnitRepo {
	return &MembershipOrgUnitRepo{
		log:      ctx.NewLoggerHelper("membership-org-unit/repo/admin-service"),
		dbClient: dbClient,
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.MembershipOrgUnit_Status, membershiporgunit.Status](
			operatorV1.MembershipOrgUnit_Status_name,
			operatorV1.MembershipOrgUnit_Status_value,
		),
	}
}

// CleanRelationsByMembershipID 清理会员组织单元关联
func (r *MembershipOrgUnitRepo) CleanRelationsByMembershipID(ctx context.Context, tx *store.Tx, membershipID uint32) error {
	if membershipID == 0 {
		return nil
	}

	if _, err := tx.MembershipOrgUnit.Delete().
		Where(
			membershiporgunit.MembershipIDEQ(membershipID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership orgUnits failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old membership orgUnits failed")
	}
	return nil
}

// CleanRelationsByMembershipIDs 清理多个会员组织单元关联
func (r *MembershipOrgUnitRepo) CleanRelationsByMembershipIDs(ctx context.Context, tx *store.Tx, membershipIDs []uint32) error {
	if len(membershipIDs) == 0 {
		return nil
	}

	if _, err := tx.MembershipOrgUnit.Delete().
		Where(
			membershiporgunit.MembershipIDIn(membershipIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership orgUnits by membership ids failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old membership orgUnits by membership ids failed")
	}
	return nil
}

// CleanRelationsByOrgUnitID 清理组织单元的会员关联
func (r *MembershipOrgUnitRepo) CleanRelationsByOrgUnitID(ctx context.Context, tx *store.Tx, orgUnitID uint32) error {
	if orgUnitID == 0 {
		return nil
	}

	if _, err := tx.MembershipOrgUnit.Delete().
		Where(
			membershiporgunit.OrgUnitIDEQ(orgUnitID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership orgUnits by orgUnit id failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old membership orgUnits by orgUnit id failed")
	}
	return nil
}

// CleanRelationsByOrgUnitIDs 清理多个组织单元的会员关联
func (r *MembershipOrgUnitRepo) CleanRelationsByOrgUnitIDs(ctx context.Context, tx *store.Tx, orgUnitIDs []uint32) error {
	if len(orgUnitIDs) == 0 {
		return nil
	}

	if _, err := tx.MembershipOrgUnit.Delete().
		Where(
			membershiporgunit.OrgUnitIDIn(orgUnitIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old membership orgUnits by orgUnit ids failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old membership orgUnits by orgUnit ids failed")
	}
	return nil
}

// RemoveOrgUnitsFromMembership 删除会员的组织单元关联
func (r *MembershipOrgUnitRepo) RemoveOrgUnitsFromMembership(ctx context.Context, membershipID uint32, ids []uint32) error {
	if membershipID == 0 || len(ids) == 0 {
		return nil
	}

	_, err := r.dbClient.Client().MembershipOrgUnit.Delete().
		Where(
			membershiporgunit.And(
				membershiporgunit.MembershipIDEQ(membershipID),
				membershiporgunit.OrgUnitIDIn(ids...),
			),
		).
		Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "remove orgUnits failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("remove orgUnits failed")
	}
	return nil
}

// AssignMembershipOrgUnits 分配组织单元给会员
func (r *MembershipOrgUnitRepo) AssignMembershipOrgUnits(
	ctx context.Context,
	tx *store.Tx,
	membershipID uint32,
	datas []*operatorV1.MembershipOrgUnit,
) error {
	if len(datas) == 0 || membershipID == 0 {
		return nil
	}

	var err error

	// 删除该角色的所有旧关联
	if err = r.CleanRelationsByMembershipID(ctx, tx, membershipID); err != nil {
		return operatorV1.ErrorIdentityInternalServerError("clean old membership orgUnits failed")
	}

	now := time.Now()

	var membershipOrgUnitCreates []*store.MembershipOrgUnitCreate
	for _, data := range datas {
		if data.StartAt == nil {
			data.StartAt = timeutil.TimeToTimestamppb(&now)
		}
		rm := tx.MembershipOrgUnit.
			Create().
			SetNillableTenantID(data.TenantId).
			SetMembershipID(data.GetMembershipId()).
			SetOrgUnitID(data.GetOrgUnitId()).
			SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
			SetNillableAssignedBy(data.AssignedBy).
			SetNillableAssignedAt(timeutil.TimestamppbToTime(data.AssignedAt)).
			SetNillableIsPrimary(data.IsPrimary).
			SetNillableStartAt(timeutil.TimestamppbToTime(data.StartAt)).
			SetNillableEndAt(timeutil.TimestamppbToTime(data.EndAt)).
			SetNillableCreatedBy(data.CreatedBy).
			SetCreatedAt(now)
		membershipOrgUnitCreates = append(membershipOrgUnitCreates, rm)
	}

	_, err = tx.MembershipOrgUnit.CreateBulk(membershipOrgUnitCreates...).Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "assign orgUnit to membership failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("assign orgUnit to membership failed")
	}

	return nil
}

// ListOrgUnitIDs 列出角色关联的组织单元ID列表
func (r *MembershipOrgUnitRepo) ListOrgUnitIDs(ctx context.Context, membershipID uint32, excludeExpired bool) ([]uint32, error) {
	if membershipID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().MembershipOrgUnit.Query().
		Where(
			membershiporgunit.MembershipIDEQ(membershipID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiporgunit.Or(
				membershiporgunit.EndAtIsNil(),
				membershiporgunit.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiporgunit.FieldOrgUnitID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query orgUnit ids by membership id failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query orgUnit ids by membership id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListMembershipIDs 获取组织单元关联的会员ID列表
func (r *MembershipOrgUnitRepo) ListMembershipIDs(ctx context.Context, orgUnitID uint32, excludeExpired bool) ([]uint32, error) {
	if orgUnitID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().MembershipOrgUnit.Query().
		Where(
			membershiporgunit.OrgUnitIDEQ(orgUnitID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiporgunit.Or(
				membershiporgunit.EndAtIsNil(),
				membershiporgunit.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiporgunit.FieldMembershipID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query membership ids by orgUnit id failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query membership ids by orgUnit id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListMembershipIDsByOrgUnitIDs 获取多个组织单元关联的会员ID列表
func (r *MembershipOrgUnitRepo) ListMembershipIDsByOrgUnitIDs(ctx context.Context, orgUnitIDs []uint32, excludeExpired bool) ([]uint32, error) {
	if len(orgUnitIDs) == 0 {
		return nil, nil
	}

	q := r.dbClient.Client().MembershipOrgUnit.Query().
		Where(
			membershiporgunit.OrgUnitIDIn(orgUnitIDs...),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			membershiporgunit.Or(
				membershiporgunit.EndAtIsNil(),
				membershiporgunit.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(membershiporgunit.FieldMembershipID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query membership ids by orgUnit ids failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query membership ids by orgUnit ids failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}
