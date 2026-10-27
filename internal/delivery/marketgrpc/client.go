package marketgrpc

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"google.golang.org/protobuf/proto"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketview"
)

type reader struct{ client marketv1.MarketDataClient }

// NewReader borrows the owned unary factory. Each call uses fresh discovery
// and verified mTLS, with no hidden transport retry or alternate source.
func NewReader(factory *rpcclient.Factory) (domain.MarketReader, error) {
	if factory == nil {
		return nil, domain.ErrMarketIdentity
	}
	connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return nil, err
	}
	return &reader{client: marketv1.NewMarketDataClient(connection)}, nil
}

func (r *reader) Reference(ctx context.Context, source domain.MarketSource, instrumentID int64, mode domain.MarketMode) (domain.MarketReference, error) {
	if err := bounded(ctx); err != nil {
		return domain.MarketReference{}, err
	}
	if !validSelection(source, instrumentID, mode) {
		return domain.MarketReference{}, domain.ErrMarketIdentity
	}
	reply, err := r.client.GetReference(ctx, &marketv1.GetReferenceRequest{
		Source:       string(source),
		InstrumentId: instrumentID,
		Mode:         string(mode),
	})
	if err != nil {
		return domain.MarketReference{}, err
	}
	if reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || proto.Size(reply) > 64*1024 {
		return domain.MarketReference{}, domain.ErrUnreliableMarket
	}
	value, err := marketview.FromProto(reply.Reference)
	if err != nil {
		return domain.MarketReference{}, err
	}
	if value.Reading.Source != source || value.Reading.InstrumentID != instrumentID || value.Reading.Mode != mode {
		return domain.MarketReference{}, domain.ErrMarketIdentity
	}
	if err := ctx.Err(); err != nil {
		return domain.MarketReference{}, err
	}
	value = currentReference(value, time.Now())
	return value, nil
}

func currentReference(value domain.MarketReference, now time.Time) domain.MarketReference {
	if value.Reading.Quality != domain.MarketQualityHealthy {
		return value
	}
	quality, reason := domain.MarketQualityHealthy, domain.MarketReasonNone
	switch {
	case !value.AuthorityUntil.After(now):
		quality, reason = domain.MarketQualityUnavailable, domain.MarketReasonDisconnected
	case value.Reading.EventTime.After(now.Add(time.Second)) || value.Reading.ReceivedAt.After(now.Add(time.Second)):
		quality, reason = domain.MarketQualityClock, domain.MarketReasonFuture
	case value.Reading.Selected(value.Reading.Source, value.Reading.InstrumentID, value.Reading.Symbol, value.Reading.Mode, now, 5*time.Second) != nil:
		quality, reason = domain.MarketQualityStale, domain.MarketReasonAge
	}
	if quality == domain.MarketQualityHealthy {
		return value
	}
	value.Reading.Quality, value.Reading.Reason = quality, reason
	value.Reading.Bid, value.Reading.Ask, value.Reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	value.Reading.BidQuantity, value.Reading.AskQuantity = decimal.Zero, decimal.Zero
	value.Reading.EvaluatedAt, value.Reading.ValidUntil = now, time.Time{}
	return value
}
