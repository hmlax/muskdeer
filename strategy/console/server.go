// Package console exposes the independent administration operator's typed boundary.
package console

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

const (
	messageForbidden        = "trusted administration operator identity is required"
	messageInvalid          = "invalid management request"
	messageUnavailable      = "management data is unavailable"
	messageNotFound         = "management record was not found"
	messageControlsDisabled = "management controls are disabled"
	messageScopeRequired    = "a bound trading account is required"
)

// Server borrows the business database and existing service clients. It never
// connects to the administration database or provisions users/permissions.
type Server struct {
	pb.UnimplementedConsoleServer
	db               *gorm.DB
	factory          *rpcclient.Factory
	logger           *logging.Logger
	controls         bool
	connection       *configv1.VenueConnection
	revisions        map[int64]int64
	accountInspector func(context.Context, *common.VenueScope) (*common.VenueSnapshot, error)
}

// Dependencies borrows one database, logger and service-client owner.
type Dependencies struct {
	DB         *gorm.DB
	Factory    *rpcclient.Factory
	Logger     *logging.Logger
	Controls   bool
	Connection *configv1.VenueConnection
	Revisions  map[int64]int64
}

// New snapshots immutable delivery dependencies without starting work.
func New(deps Dependencies) (*Server, error) {
	if deps.DB == nil || deps.Logger == nil || deps.Factory == nil {
		return nil, errors.New(messageInvalid)
	}
	revisions := make(map[int64]int64, len(deps.Revisions))
	for id, revision := range deps.Revisions {
		revisions[id] = revision
	}
	return &Server{
		db:         deps.DB,
		factory:    deps.Factory,
		logger:     deps.Logger,
		controls:   deps.Controls,
		connection: proto.CloneOf(deps.Connection),
		revisions:  revisions,
	}, nil
}

func (s *Server) authorize(ctx context.Context, actor *pb.Actor, write bool) error {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceOperator) || strings.TrimSpace(actor.GetSubject()) == "" || len(actor.GetSubject()) > 64 {
		return status.Error(codes.PermissionDenied, messageForbidden)
	}
	if write && !s.controls {
		return status.Error(codes.PermissionDenied, messageControlsDisabled)
	}
	if write && (len(actor.GetRequestId()) < 16 || len(actor.GetRequestId()) > 128) {
		return status.Error(codes.InvalidArgument, messageInvalid)
	}
	return nil
}

func (s *Server) fail(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Error(codes.NotFound, messageNotFound)
	}
	if logErr := s.logger.LogAttrs(ctx, slog.LevelError, messageUnavailable, slog.Any("cause", err)); logErr != nil {
		return status.Error(codes.Unavailable, messageUnavailable)
	}
	return status.Error(codes.Unavailable, messageUnavailable)
}

func fresh(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}

func pagination(page, size uint32) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if size > 200 || page > 10000000 {
		return 0, 0, status.Error(codes.InvalidArgument, messageInvalid)
	}
	return int(page-1) * int(size), int(size), nil
}

// accountScope must run before any account-owned query. The trusted operator
// supplies bindings, and an empty merchant scope can never become global.
func accountScope(actor *pb.Actor) error {
	if actor.GetTenantId() == 0 {
		return nil
	}
	if len(actor.GetAllowedAccountIds()) == 0 {
		return status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	seen := make(map[string]bool)
	for _, account := range actor.AllowedAccountIds {
		if strings.TrimSpace(account) == "" || len(account) > 64 || seen[account] {
			return status.Error(codes.PermissionDenied, messageScopeRequired)
		}
		seen[account] = true
	}
	return nil
}

func scoped(query *gorm.DB, actor *pb.Actor) *gorm.DB {
	if actor.GetTenantId() > 0 {
		return query.Where("account_id IN ?", actor.AllowedAccountIds)
	}
	return query
}
