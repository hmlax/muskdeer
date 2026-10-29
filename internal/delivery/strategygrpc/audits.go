package strategygrpc

import (
	"context"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// AuditServer borrows platform history queries without owning tasks or effects.
type AuditServer struct {
	strategyv1.UnimplementedStrategyAuditServer
	audits domain.AdminAudits
}

// NewAudit constructs the API-peer-only current-viewer history boundary.
func NewAudit(audits domain.AdminAudits) (*AuditServer, error) {
	if audits == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &AuditServer{audits: audits}, nil
}

// ListAudits returns a bounded exact-filter page with explicit historical fields.
func (s *AuditServer) ListAudits(ctx context.Context, request *strategyv1.ListAuditsRequest) (*strategyv1.ListAuditsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	filter := domain.AdminAuditFilter{
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		Actor:    request.Actor,
		Action:   request.Action,
		Resource: request.Resource,
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.audits.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit || page.NextAfterID < 0 {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListAuditsReply{
		Audits:      make([]*strategyv1.AuditEntry, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	previous := request.AfterId
	for _, entry := range page.Items {
		if entry.ID <= previous || (filter.Actor != "" && entry.Actor != filter.Actor) || (filter.Action != "" && entry.Action != filter.Action) || (filter.Resource != "" && entry.Resource != filter.Resource) {
			return nil, rpcError(domain.ErrAdminUnavailable)
		}
		value, err := auditProto(entry)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Audits = append(reply.Audits, value)
		previous = entry.ID
	}
	if page.NextAfterID != 0 && (len(page.Items) != filter.Page.Limit || page.NextAfterID != previous) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetAudit returns exactly one historical record, never an operation completion.
func (s *AuditServer) GetAudit(ctx context.Context, request *strategyv1.GetAuditRequest) (*strategyv1.GetAuditReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if request.AuditId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	entry, err := s.audits.Detail(ctx, request.Session.Token, request.AuditId)
	if err != nil {
		return nil, rpcError(err)
	}
	if entry.ID != request.AuditId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	value, err := auditProto(entry)
	if err != nil {
		return nil, rpcError(err)
	}
	return &strategyv1.GetAuditReply{Audit: value}, nil
}

func auditProto(entry domain.AdminAuditEntry) (*strategyv1.AuditEntry, error) {
	if entry.ID <= 0 || !entry.ActorRole.Allows(domain.AdminRoleViewer) || entry.CreatedAt.UnixMilli() <= 0 ||
		!auditText(entry.Actor, 64) || !auditText(entry.Action, 64) || !auditText(entry.Resource, 128) || !auditText(entry.Reason, 256) ||
		(entry.BeforeVersion != nil && *entry.BeforeVersion <= 0) || (entry.AfterVersion != nil && *entry.AfterVersion <= 0) {
		return nil, domain.ErrAdminUnavailable
	}
	result := &strategyv1.AuditEntry{
		Id:          entry.ID,
		Actor:       entry.Actor,
		ActorRole:   string(entry.ActorRole),
		Action:      entry.Action,
		Resource:    entry.Resource,
		Reason:      entry.Reason,
		CreatedAtMs: entry.CreatedAt.UnixMilli(),
	}
	if entry.BeforeVersion != nil {
		result.BeforeVersion = proto.Int64(*entry.BeforeVersion)
	}
	if entry.AfterVersion != nil {
		result.AfterVersion = proto.Int64(*entry.AfterVersion)
	}
	return result, nil
}

func auditText(value string, maximum int) bool {
	return len(value) > 0 && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

var _ strategyv1.StrategyAuditServer = (*AuditServer)(nil)
