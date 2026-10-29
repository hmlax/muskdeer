package strategygrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type auditStore struct {
	calls  int
	token  string
	filter domain.AdminAuditFilter
	page   domain.AdminAuditPage
	entry  domain.AdminAuditEntry
	err    error
}

func (s *auditStore) List(_ context.Context, token string, filter domain.AdminAuditFilter) (domain.AdminAuditPage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return s.page, s.err
}

func (s *auditStore) Detail(_ context.Context, token string, _ int64) (domain.AdminAuditEntry, error) {
	s.calls++
	s.token = token
	return s.entry, s.err
}

func auditEntryFixture() domain.AdminAuditEntry {
	return domain.AdminAuditEntry{
		ID:            9007199254740993,
		Actor:         "history-actor",
		ActorRole:     domain.AdminRoleOperator,
		Action:        domain.AdminStrategyPublish,
		Resource:      domain.AdminAuditResourceStrategy + ":9007199254740993",
		Reason:        "历史原因 <script> is text",
		BeforeVersion: proto.Int64(9007199254740993),
		AfterVersion:  proto.Int64(9007199254740994),
		CreatedAt:     time.UnixMilli(1793200000000).UTC(),
	}
}

func TestAuditGRPCKeepsExactHistoryVersionsAndCurrentAuthorityFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	entry := auditEntryFixture()
	store := &auditStore{
		entry: entry,
		page: domain.AdminAuditPage{
			Items:       []domain.AdminAuditEntry{entry},
			NextAfterID: entry.ID,
		},
	}
	server, err := NewAudit(store)
	if err != nil {
		t.Fatal(err)
	}
	request := &strategyv1.ListAuditsRequest{
		Session:  &strategyv1.AdminSession{Token: testSessionToken},
		AfterId:  entry.ID - 1,
		Limit:    1,
		Actor:    entry.Actor,
		Action:   entry.Action,
		Resource: entry.Resource,
	}
	page, err := server.ListAudits(ctx, request)
	if err != nil || page.NextAfterId != entry.ID || len(page.Audits) != 1 || page.Audits[0].GetAfterVersion() != *entry.AfterVersion || page.Audits[0].Reason != entry.Reason || store.token != testSessionToken || store.filter.Resource != request.Resource || store.filter.Page.AfterID != request.AfterId {
		t.Fatal("audit history lost exact revisions, filters or current session", err)
	}
	*page.Audits[0].AfterVersion = 1
	if *entry.AfterVersion != 9007199254740994 {
		t.Fatal("wire response borrowed the domain revision pointer")
	}
	store.entry.BeforeVersion = nil
	detail, err := server.GetAudit(ctx, &strategyv1.GetAuditRequest{
		Session: request.Session,
		AuditId: entry.ID,
	})
	if err != nil || detail.Audit.BeforeVersion != nil || detail.Audit.Id != entry.ID || detail.Audit.ActorRole != string(domain.AdminRoleOperator) || detail.Audit.CreatedAtMs != entry.CreatedAt.UnixMilli() {
		t.Fatal("audit detail invented an absent version or changed historical identity", err)
	}
	store.err = domain.ErrUnauthenticated
	if _, err := server.ListAudits(ctx, request); status.Code(err) != codes.Unauthenticated {
		t.Fatal("audit boundary ignored current session revocation", err)
	}
	store.err = errors.New("private audit persistence failure")
	if _, err := server.GetAudit(ctx, &strategyv1.GetAuditRequest{
		Session: request.Session,
		AuditId: entry.ID,
	}); status.Code(err) != codes.Unavailable || status.Convert(err).Message() != domain.MessageAdminUnavailable {
		t.Fatal("audit boundary exposed storage diagnostics", err)
	}
}

func TestAuditGRPCRejectsUntrustedPeerAndAmbiguousInputBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &auditStore{}
	server, err := NewAudit(store)
	if err != nil {
		t.Fatal(err)
	}
	request := &strategyv1.ListAuditsRequest{
		Session: &strategyv1.AdminSession{Token: testSessionToken},
		Limit:   1,
	}
	for _, peer := range []context.Context{ctx, verified(ctx, rpcsecurity.ServicePortfolio), verified(ctx, rpcsecurity.ServiceStrategy)} {
		if _, err := server.ListAudits(peer, request); status.Code(err) != codes.PermissionDenied {
			t.Fatal("non-API peer reached platform audit history", err)
		}
	}
	if _, err := server.ListAudits(verified(context.WithoutCancel(ctx), rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded audit request accepted", err)
	}
	api := verified(ctx, rpcsecurity.ServiceAPI)
	for _, change := range []func(*strategyv1.ListAuditsRequest){
		func(r *strategyv1.ListAuditsRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListAuditsRequest) { r.Limit = domain.MaxAdminPageSize + 1 },
		func(r *strategyv1.ListAuditsRequest) { r.Resource = strings.Repeat("x", 129) },
		func(r *strategyv1.ListAuditsRequest) { r.Actor = " space" },
		func(r *strategyv1.ListAuditsRequest) { r.Action = "line\nfeed" },
		func(r *strategyv1.ListAuditsRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		candidate := proto.Clone(request).(*strategyv1.ListAuditsRequest)
		change(candidate)
		if _, err := server.ListAudits(api, candidate); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid audit input reached history storage", err)
		}
	}
	request.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.ListAudits(api, request); status.Code(err) != codes.Unauthenticated {
		t.Fatal("unknown audit credential field accepted", err)
	}
	if _, err := server.GetAudit(api, &strategyv1.GetAuditRequest{AuditId: 0}); status.Code(err) != codes.InvalidArgument || store.calls != 0 {
		t.Fatal("invalid audit identity reached storage", err)
	}
}

func TestAuditGRPCRejectsMismatchedHistoryAndPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	for _, change := range []func(*auditStore){
		func(s *auditStore) { s.page.NextAfterID++ },
		func(s *auditStore) { s.page.Items = append(s.page.Items, s.page.Items[0]) },
		func(s *auditStore) { s.page.Items[0].ID = 1 },
		func(s *auditStore) { s.page.Items[0].Resource = "another-resource" },
		func(s *auditStore) { s.page.Items[0].ActorRole = "unknown" },
		func(s *auditStore) { s.page.Items[0].BeforeVersion = proto.Int64(0) },
		func(s *auditStore) { s.page.Items[0].Reason = string([]byte{0xff}) },
	} {
		entry := auditEntryFixture()
		store := &auditStore{page: domain.AdminAuditPage{
			Items:       []domain.AdminAuditEntry{entry},
			NextAfterID: entry.ID,
		}}
		change(store)
		server, err := NewAudit(store)
		if err != nil {
			t.Fatal(err)
		}
		if reply, err := server.ListAudits(ctx, &strategyv1.ListAuditsRequest{
			Session:  &strategyv1.AdminSession{Token: testSessionToken},
			AfterId:  entry.ID - 1,
			Limit:    1,
			Resource: entry.Resource,
		}); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("invalid audit history or cursor crossed the wire", err)
		}
	}
	store := &auditStore{entry: auditEntryFixture()}
	server, err := NewAudit(store)
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := server.GetAudit(ctx, &strategyv1.GetAuditRequest{
		Session: &strategyv1.AdminSession{Token: testSessionToken},
		AuditId: store.entry.ID + 1,
	}); status.Code(err) != codes.Unavailable || reply != nil {
		t.Fatal("audit detail returned another historical identity", err)
	}
}
