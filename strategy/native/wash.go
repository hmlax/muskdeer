package native

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

type washObservation struct {
	book  *common.VenueBook
	trade *common.VenueTrade
}

func washWorkers(cfg *venueconfig.RuntimeConfig, factory *rpcclient.Factory, logger *logging.Logger) []deer.Worker {
	if !cfg.Venue.GetSyntheticTape().GetEnabled() {
		return nil
	}
	var workers []deer.Worker
	for _, profile := range cfg.Venue.SyntheticTape.Profiles {
		workers = append(workers, deer.Worker{
			Name: values.SyntheticWorker + profile.Symbol,
			Run: func(ctx context.Context) error {
				for ctx.Err() == nil {
					err := factory.Subscribe(ctx, rpcclient.Target{Service: rpcsecurity.ServiceMarket}, func(work context.Context, conn grpc.ClientConnInterface) error {
						return subscribeWash(work, cfg, profile.Symbol, market.NewMarketClient(conn), func(send context.Context, input *execution.WashTradeRequest) error {
							client, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
							if err != nil {
								return err
							}
							reply, err := execution.NewExecutionClient(client).WashTrade(send, input)
							if err != nil {
								return err
							}
							if reply.GetState() != values.SyntheticPublished && reply.GetState() != values.SyntheticUnknown {
								return values.ErrInvalid
							}
							return nil
						}, logger)
					})
					if err != nil && ctx.Err() == nil {
						if err := logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("symbol", profile.Symbol)); err != nil {
							return err
						}
					}
					if err := values.Wait(ctx, cfg.Venue.SyntheticTape.Interval.AsDuration()); err != nil {
						return err
					}
				}
				return ctx.Err()
			},
		})
	}
	return workers
}

// Only this subscription scope owns the receiver goroutine. The one-frame
// buffer coalesces source bursts while SDK publication is in progress.
func subscribeWash(ctx context.Context, cfg *venueconfig.RuntimeConfig, symbol string, client market.MarketClient, send func(context.Context, *execution.WashTradeRequest) error, logger *logging.Logger) error {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	var receive func() (*washObservation, error)
	if cfg.Venue.SyntheticTape.Mode == config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		stream, err := client.SubscribeTrades(work, &market.TradeRequest{Symbol: symbol})
		if err != nil {
			return err
		}
		receive = func() (*washObservation, error) { v, err := stream.Recv(); return &washObservation{trade: v}, err }
	} else {
		stream, err := client.SubscribeBooks(work, &market.BookRequest{Symbol: symbol})
		if err != nil {
			return err
		}
		receive = func() (*washObservation, error) { v, err := stream.Recv(); return &washObservation{book: v}, err }
	}
	updates := make(chan *washObservation, 1)
	failure := make(chan error, 1)
	var joined sync.WaitGroup
	joined.Go(func() {
		for {
			observation, err := receive()
			if err != nil {
				failure <- err
				return
			}
			select {
			case updates <- observation:
			default:
				select {
				case <-updates:
				default:
				}
				select {
				case updates <- observation:
				case <-work.Done():
					return
				}
			}
		}
	})
	defer func() { cancel(); joined.Wait() }()
	ticker := time.NewTicker(cfg.Venue.SyntheticTape.Interval.AsDuration())
	defer ticker.Stop()
	var last int64
	side := domain.SideBuy
	for {
		select {
		case <-work.Done():
			return work.Err()
		case err := <-failure:
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		case <-ticker.C:
			select {
			case observation := <-updates:
				sourceID := observation.book.GetSequence()
				if observation.trade != nil {
					var err error
					sourceID, err = values.TradeSequence(observation.trade)
					if err != nil {
						continue
					}
				}
				if sourceID <= last {
					continue
				}
				last = sourceID
				input := &execution.WashTradeRequest{
					StrategyInstanceId: cfg.InstanceID,
					StrategyRevision:   cfg.Revision,
					Symbol:             symbol,
					Side:               string(side),
				}
				if observation.trade != nil {
					input.Source = &execution.WashTradeRequest_Trade{Trade: observation.trade}
				} else {
					input.Source = &execution.WashTradeRequest_Book{Book: observation.book}
				}
				target, err := values.WashTarget(cfg.Venue, input)
				if err != nil {
					continue
				}
				input.Side, input.Price, input.Quantity = target.Side, target.Price, target.Quantity
				op, end := context.WithTimeout(work, cfg.OperationTimeout)
				err = send(op, input)
				end()
				if side == domain.SideBuy {
					side = domain.SideSell
				} else {
					side = domain.SideBuy
				}
				if err != nil && work.Err() == nil && status.Code(err) != codes.FailedPrecondition && status.Code(err) != codes.ResourceExhausted {
					if err := logger.LogAttrs(work, slog.LevelWarn, values.StateRecovering, slog.String("symbol", symbol)); err != nil {
						return err
					}
				}
			default:
			}
		}
	}
}
