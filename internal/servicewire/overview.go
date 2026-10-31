// Package servicewire maps explicit service observations to management DTOs.
package servicewire

import (
	"time"

	"google.golang.org/protobuf/proto"

	adminv1 "github.com/hmlax/muskdeer/api/admin/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// OverviewProto rejects malformed successes without returning partial members.
// Sample times remain independent; no wall-clock freshness or execution grant
// is inferred from membership, declared versions or readiness.
func OverviewProto(value domain.AdminServiceOverview, filter domain.AdminServiceFilter) (*adminv1.ServiceOverview, error) {
	if filter.Validate() != nil || value.Role != filter.Role || !validScope(value) || !exactMillis(value.MembershipObservedAt) ||
		len(value.Items) > filter.Limit || int64(len(value.Items)) > value.Total {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &adminv1.ServiceOverview{
		Role:                   string(value.Role),
		MembershipSource:       string(value.Source),
		MembershipObservedAtMs: value.MembershipObservedAt.UnixMilli(),
		Total:                  value.Total,
		ProbeConfigured:        value.ProbeConfigured,
		Items:                  make([]*adminv1.ServiceInstanceObservation, 0, len(value.Items)),
		NextAfterInstanceId:    value.NextAfterInstanceID,
	}
	previous := filter.AfterInstanceID
	for _, item := range value.Items {
		if !validInstance(item, value.Role, value.ProbeConfigured) || item.InstanceID <= previous {
			return nil, domain.ErrAdminUnavailable
		}
		row := &adminv1.ServiceInstanceObservation{
			InstanceId:      item.InstanceID,
			DeclaredVersion: item.DeclaredVersion,
			Status:          string(item.Status),
		}
		if item.ProbedAt != nil {
			row.ProbedAtMs = proto.Int64(item.ProbedAt.UnixMilli())
		}
		reply.Items = append(reply.Items, row)
		previous = item.InstanceID
	}
	if !validCursor(value, filter, previous) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func validScope(value domain.AdminServiceOverview) bool {
	if value.Role == domain.AdminServiceAPI {
		return value.Source == domain.AdminServiceLocalAPI && value.Total == 1 && value.ProbeConfigured
	}
	return value.Source == domain.AdminServiceRegistry && value.Total >= 0 && value.Total <= domain.MaxAdminServiceInstances
}

func validInstance(item domain.AdminServiceInstance, role domain.AdminServiceRole, configured bool) bool {
	if !domain.ValidAdminServiceLabel(item.InstanceID) || !domain.ValidAdminServiceLabel(item.DeclaredVersion) || !item.Status.Valid() ||
		role != domain.AdminServiceAPI && !domain.ValidAdminRemoteInstanceID(item.InstanceID) {
		return false
	}
	if !configured {
		return item.Status == domain.AdminServiceProbeNotConfigured && item.ProbedAt == nil
	}
	if item.Status == domain.AdminServiceProbeNotConfigured || item.ProbedAt == nil || !exactMillis(*item.ProbedAt) {
		return false
	}
	return role != domain.AdminServiceAPI || item.Status == domain.AdminServiceReady || item.Status == domain.AdminServiceNotReady || item.Status == domain.AdminServiceProbeTimeout
}

func validCursor(value domain.AdminServiceOverview, filter domain.AdminServiceFilter, last string) bool {
	count := int64(len(value.Items))
	if filter.AfterInstanceID == "" && (count < value.Total && len(value.Items) < filter.Limit ||
		(value.NextAfterInstanceID != "") != (value.Total > count)) {
		return false
	}
	return value.NextAfterInstanceID == "" || value.Total > count && len(value.Items) == filter.Limit && value.NextAfterInstanceID == last
}

func exactMillis(value time.Time) bool {
	return value.UnixMilli() > 0 && value.Equal(time.UnixMilli(value.UnixMilli()))
}
