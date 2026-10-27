// Package marketview encodes normalized consumer market references with concrete
// protobuf fields. Diagnostic baselines never cross this consumption boundary.
package marketview

import (
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	marketv1 "github.com/hmlax/muskdeer/api/market/v1"
	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

const wireZero = "0"

// ToProto formats only validated finite decimals; unhealthy values are zero.
func ToProto(input domain.MarketReference) (*marketv1.MarketReference, error) {
	if err := validate(input); err != nil {
		return nil, err
	}
	r := input.Reading
	view := &marketv1.MarketReference{
		FeedId:              input.FeedID,
		Source:              string(r.Source),
		InstrumentId:        r.InstrumentID,
		ExternalSymbol:      r.Symbol,
		Mode:                string(r.Mode),
		SourceVersion:       r.SourceVersion,
		PublisherOwnerId:    input.OwnerID,
		PublisherEpoch:      input.OwnerEpoch,
		PublicationRevision: input.Revision,
		AuthorityUntilMs:    milliseconds(input.AuthorityUntil),
		Connection:          r.Connection,
		Generation:          r.Generation,
		Sequence:            r.Sequence,
		Quality:             string(r.Quality),
		Reason:              string(r.Reason),
		BidPrice:            wireZero,
		BidQuantity:         wireZero,
		AskPrice:            wireZero,
		AskQuantity:         wireZero,
		MidPrice:            wireZero,
		EventTimeMs:         milliseconds(r.EventTime),
		ReceivedAtMs:        milliseconds(r.ReceivedAt),
		EvaluatedAtMs:       milliseconds(r.EvaluatedAt),
		ValidUntilMs:        milliseconds(r.ValidUntil),
	}
	if r.Quality == domain.MarketQualityHealthy {
		view.BidPrice, view.AskPrice, view.MidPrice = r.Bid.String(), r.Ask.String(), r.Mid.String()
		view.BidQuantity, view.AskQuantity = r.BidQuantity.String(), r.AskQuantity.String()
	}
	return view, nil
}

// FromProto rejects unknown fields, wrong shape and decimal syntax. It does not
// extend the encoded expiry; consumers still use Selected at the use boundary.
func FromProto(view *marketv1.MarketReference) (domain.MarketReference, error) {
	if view == nil || len(view.ProtoReflect().GetUnknown()) != 0 || proto.Size(view) > 64*1024 ||
		view.EventTimeMs < 0 || view.ReceivedAtMs < 0 || view.EvaluatedAtMs <= 0 || view.ValidUntilMs < 0 || view.AuthorityUntilMs < 0 {
		return domain.MarketReference{}, domain.ErrUnreliableMarket
	}
	r := domain.MarketReading{
		Source:        domain.MarketSource(view.Source),
		InstrumentID:  view.InstrumentId,
		Symbol:        view.ExternalSymbol,
		Mode:          domain.MarketMode(view.Mode),
		SourceVersion: view.SourceVersion,
		Connection:    view.Connection,
		Generation:    view.Generation,
		Sequence:      view.Sequence,
		Quality:       domain.MarketQuality(view.Quality),
		Reason:        domain.MarketReason(view.Reason),
		EventTime:     instant(view.EventTimeMs),
		ReceivedAt:    instant(view.ReceivedAtMs),
		EvaluatedAt:   instant(view.EvaluatedAtMs),
		ValidUntil:    instant(view.ValidUntilMs),
	}
	if err := prices(view, &r); err != nil {
		return domain.MarketReference{}, err
	}
	result := domain.MarketReference{
		FeedID:         view.FeedId,
		OwnerID:        view.PublisherOwnerId,
		OwnerEpoch:     view.PublisherEpoch,
		AuthorityUntil: instant(view.AuthorityUntilMs),
		Revision:       view.PublicationRevision,
		Reading:        r,
	}
	return result, validate(result)
}

func validate(input domain.MarketReference) error {
	if input.FeedID <= 0 || input.OwnerEpoch <= 0 || input.Revision <= 0 || !ownerid.Valid(input.OwnerID) {
		return domain.ErrUnreliableMarket
	}
	if err := input.Reading.Validate(); err != nil {
		return err
	}
	if input.Reading.Quality == domain.MarketQualityHealthy && (input.AuthorityUntil.UnixMilli() <= 0 || input.Reading.ValidUntil.After(input.AuthorityUntil) || !input.AuthorityUntil.After(input.Reading.EvaluatedAt)) {
		return domain.ErrUnreliableMarket
	}
	return nil
}

func prices(view *marketv1.MarketReference, reading *domain.MarketReading) error {
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
		AllowZero:     true,
	}
	for _, field := range []struct {
		value  string
		target *decimal.Decimal
	}{
		{
			value:  view.BidPrice,
			target: &reading.Bid,
		},
		{
			value:  view.BidQuantity,
			target: &reading.BidQuantity,
		},
		{
			value:  view.AskPrice,
			target: &reading.Ask,
		},
		{
			value:  view.AskQuantity,
			target: &reading.AskQuantity,
		},
	} {
		value, err := domain.ParseDecimal(field.value, rule)
		if err != nil {
			return err
		}
		*field.target = value
	}
	var err error
	reading.Mid, err = domain.ParseLedgerDecimal(view.MidPrice, false, true)
	return err
}

func milliseconds(value time.Time) int64 {
	if value.IsZero() || value.UnixMilli() <= 0 {
		return 0
	}
	return value.UnixMilli()
}

func instant(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
