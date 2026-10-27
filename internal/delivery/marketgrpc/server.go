// Package marketgrpc delivers exact-source authenticated market observations.
package marketgrpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketview"
)

// Fixed public RPC messages and maximum native observation deadline.
const (
	MessageInvalid     = "invalid market reference request"
	MessageDenied      = "service is not allowed to read market references"
	MessageUnavailable = "market reference operation failed"
	MaximumCall        = 5 * time.Second
)

// FeedLookup exposes no creation or rebinding capability to the handler.
type FeedLookup interface {
	Find(context.Context, domain.MarketSource, int64, domain.MarketMode) (domain.MarketFeedDefinition, error)
}

// SnapshotReader exposes no publication or baseline update capability.
type SnapshotReader interface {
	Get(context.Context, int64) (domain.MarketPublication, error)
}

// Dependencies are borrowed and contain no unbounded worker/resource ownership.
type Dependencies struct {
	Feeds     FeedLookup
	Snapshots SnapshotReader
}

// Server implements every declared read method with verified service identity.
type Server struct {
	marketv1.UnimplementedMarketDataServer
	deps Dependencies
}

var _ marketv1.MarketDataServer = (*Server)(nil)

// New performs no I/O, global registration or background work.
func New(deps Dependencies) (*Server, error) {
	if deps.Feeds == nil || deps.Snapshots == nil {
		return nil, domain.ErrMarketIdentity
	}
	return &Server{deps: deps}, nil
}

// GetReference selects exactly the configured source/product/mode and reads a
// current fenced projection. Unhealthy quality remains explicit in the reply.
func (s *Server) GetReference(ctx context.Context, request *marketv1.GetReferenceRequest) (*marketv1.GetReferenceReply, error) {
	if !authorized(ctx) {
		return nil, status.Error(codes.PermissionDenied, MessageDenied)
	}
	if err := bounded(ctx); err != nil {
		return nil, rpcError(err)
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || proto.Size(request) > 1024 || !validSelection(domain.MarketSource(request.Source), request.InstrumentId, domain.MarketMode(request.Mode)) {
		return nil, status.Error(codes.InvalidArgument, MessageInvalid)
	}
	feed, err := s.deps.Feeds.Find(ctx, domain.MarketSource(request.Source), request.InstrumentId, domain.MarketMode(request.Mode))
	if err != nil {
		return nil, rpcError(err)
	}
	if feed.ID <= 0 || feed.Source != domain.MarketSource(request.Source) || feed.InstrumentID != request.InstrumentId || feed.Mode != domain.MarketMode(request.Mode) {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	publication, err := s.deps.Snapshots.Get(ctx, feed.ID)
	if err != nil {
		return nil, rpcError(err)
	}
	reading := publication.Reading
	if publication.FeedID != feed.ID || reading.Source != feed.Source || reading.InstrumentID != feed.InstrumentID || reading.Mode != feed.Mode || reading.Symbol != feed.Symbol || reading.SourceVersion != feed.SourceVersion {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	view, err := marketview.ToProto(publication.MarketReference)
	if err != nil {
		return nil, status.Error(codes.Internal, MessageUnavailable)
	}
	return &marketv1.GetReferenceReply{Reference: view}, nil
}

func authorized(ctx context.Context) bool {
	for _, service := range []string{rpcsecurity.ServiceStrategy, rpcsecurity.ServiceExecution, rpcsecurity.ServicePortfolio, rpcsecurity.ServiceAPI, rpcsecurity.ServiceCron} {
		if rpcsecurity.HasService(ctx, service) {
			return true
		}
	}
	return false
}

func bounded(ctx context.Context) error {
	if ctx == nil {
		return domain.ErrMarketIdentity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > MaximumCall {
		return domain.ErrMarketIdentity
	}
	return nil
}

func validSelection(source domain.MarketSource, instrumentID int64, mode domain.MarketMode) bool {
	return source.ValidateSelection() == nil && instrumentID > 0 && (mode == domain.MarketModeLive || mode == domain.MarketModeReplay)
}

func rpcError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	case errors.Is(err, domain.ErrMarketIdentity):
		return status.Error(codes.InvalidArgument, MessageInvalid)
	case errors.Is(err, domain.ErrMarketFeedMissing), errors.Is(err, domain.ErrMarketMissing):
		return status.Error(codes.NotFound, domain.MessageMarketMissing)
	default:
		return status.Error(codes.Unavailable, MessageUnavailable)
	}
}
