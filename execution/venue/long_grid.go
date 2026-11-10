package venue

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"github.com/shopspring/decimal"
)

const gridLogSubmitted = "LONG_GRID_SUBMITTED"

// EvaluateLongGrid accepts an observation, never caller-selected orders or size.
func (r *Instances) EvaluateLongGrid(ctx context.Context, in *pb.LongGridRequest) (*pb.LongGridResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	child, err := r.selected(in.GetStrategyInstanceId(), in.GetStrategyRevision(), false)
	if err != nil {
		return nil, err
	}
	if child.cfg.LongGrid == nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	return child.evaluateLongGrid(ctx, in.GetBook(), true)
}

func nowMillis() int64 { return time.Now().UnixMilli() }
func gridScope(profile *common.VenueProfilePolicy) gateway.Scope {
	return gateway.Scope{
		UserID: profile.UserId,
		FundID: profile.FundId,
		Symbol: profile.Symbol,
	}
}
func validGridBook(book *common.VenueBook, profile *common.VenueProfilePolicy) bool {
	return book.GetSymbol() == profile.Symbol && book.GetSource() == profile.MarketDataSource && book.GetSegment() == native.Segment && book.GetPublisher().GetAccountId() == native.MarketKey(profile.Symbol) && book.GetValidUntilMs() > nowMillis() && len(book.GetBids()) > 0 && len(book.GetAsks()) > 0
}

func (r *Runtime) evaluateLongGrid(ctx context.Context, book *common.VenueBook, opening bool) (*pb.LongGridResult, error) {
	profile := r.cfg.Venue.Profiles[0]
	leave, err := r.enter(native.Scope(profile))
	if err != nil {
		return nil, err
	}
	defer leave()
	client, ok := r.client.Load().Client.(gateway.LongGridClient)
	if !ok {
		return nil, ErrInvalidConfig
	}
	control, err := r.gridControl(ctx)
	if err != nil {
		return nil, err
	}
	raw, snapshot, err := r.read(ctx, profile)
	if err != nil {
		return nil, err
	}
	state, err := r.gridCursor(ctx, control, snapshot)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return &pb.LongGridResult{State: longgrid.Stopped}, nil
	}
	state, err = r.rearmGrid(ctx, state, control, snapshot)
	if err != nil {
		return nil, err
	}
	if state.State == longgrid.Stopped {
		return &pb.LongGridResult{State: longgrid.Stopped}, nil
	}
	resolving := state.PendingActionId != nil || state.StopActionId != nil
	pendingErr := errors.Join(r.resolveGridAction(ctx, state.PendingActionId, client, raw.BasePrecision), r.resolveGridAction(ctx, state.StopActionId, client, raw.BasePrecision))
	if resolving {
		if err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Where("id = ?", state.Id).Take(state).Error; err != nil {
			return nil, err
		}
		_, snapshot, err = r.read(ctx, profile)
		if err != nil {
			return nil, err
		}
	}
	rules, err := longgrid.Parse(r.cfg.LongGrid)
	if err != nil {
		return nil, err
	}
	bid := ""
	if validGridBook(book, profile) {
		bid = book.Bids[0].Price
	}
	stop, err := longgrid.Decide(rules, state.Lots, decimal.Zero, state.Started, snapshot, bid, "", false, state.State == longgrid.Stopping)
	if err != nil {
		return nil, err
	}
	if stop.Kind == longgrid.StopLoss || state.State == longgrid.Stopping {
		return r.runGridStop(ctx, state, snapshot, stop, client)
	}
	if pendingErr != nil || state.PendingActionId != nil {
		return &pb.LongGridResult{State: longgrid.Unknown}, nil
	}
	if !opening || !control.DesiredRunning || !validGridBook(book, profile) {
		return &pb.LongGridResult{State: longgrid.Running}, nil
	}
	next, err := domain.ParseLedgerDecimal(state.NextBuyPrice, false, true)
	if err != nil {
		return nil, err
	}
	decision, err := longgrid.Decide(rules, state.Lots, next, state.Started, snapshot, book.Bids[0].Price, book.Asks[0].Price, true, false)
	if err != nil {
		return nil, err
	}
	if decision.Kind == longgrid.Hold {
		return &pb.LongGridResult{
			State:  longgrid.Running,
			Action: longgrid.Hold,
		}, nil
	}
	action, err := r.claimGridAction(ctx, state, snapshot, book, decision)
	if err != nil {
		return nil, err
	}
	expires := snapshot.ValidUntilMs
	if decision.Side == domain.SideBuy {
		expires = min(expires, book.ValidUntilMs)
	}
	return r.sendGridAction(ctx, action, client, expires)
}

func (r *Runtime) sendGridAction(ctx context.Context, action *model.LongGridAction, client gateway.LongGridClient, expires int64) (*pb.LongGridResult, error) {
	profile := r.cfg.Venue.Profiles[0]
	price, e1 := domain.ParseLedgerDecimal(action.Price, false, true)
	quantity, e2 := domain.ParseLedgerDecimal(action.Quantity, false, false)
	if e1 != nil || e2 != nil {
		return nil, longgrid.ErrInvalid
	}
	op, cancelOperation := context.WithDeadline(ctx, time.UnixMilli(expires-25))
	defer cancelOperation()
	var response *gateway.SubmitResult
	var sendErr error
	if op.Err() != nil {
		sendErr = errors.Join(gateway.ErrNotDispatched, op.Err())
	} else {
		response, sendErr = client.SubmitGrid(op, gateway.GridRequest{
			Scope:      gridScope(profile),
			ClientID:   action.ClientId,
			Side:       domain.Side(action.Side),
			Price:      price,
			Quantity:   quantity,
			PositionID: action.PositionId,
			Leverage:   profile.Leverage,
			ReduceOnly: action.Side == string(domain.SideSell),
			Market:     action.Kind == longgrid.StopLoss,
		})
	}
	action.State = longgrid.Unknown
	if sendErr == nil && response.GetOrderID() > 0 && response.GetClientID() == action.ClientId {
		action.State = longgrid.Acknowledged
		action.OrderId = strconv.FormatUint(response.OrderID, 10)
	} else if definitiveGridRejection(sendErr) {
		action.State = longgrid.Rejected
	}
	// A caller deadline may expire after SDK dispatch. Persist only the receipt
	// under a bounded cleanup scope; this never sends another trading request.
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := gridDB(persist, r.db).Model(&model.LongGridAction{}).Where("id = ? AND state = ?", action.Id, longgrid.Dispatched).Select("State", "OrderId").Updates(action).Error; err != nil {
		return nil, err
	}
	if err := r.logger.LogAttrs(ctx, slog.LevelInfo, gridLogSubmitted, slog.String("symbol", profile.Symbol), slog.String("action", action.Kind), slog.String("state", action.State), slog.String("reason", status.Code(sendErr).String())); err != nil {
		return nil, err
	}
	return &pb.LongGridResult{
		State:   action.State,
		Action:  action.Kind,
		OrderId: action.OrderId,
	}, nil
}

func definitiveGridRejection(err error) bool {
	if err == nil || errors.Is(err, domain.ErrOutcomeUnknown) {
		return false
	}
	if errors.Is(err, gateway.ErrNotDispatched) {
		return true
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated:
		return true
	default:
		return false
	}
}

func (r *Runtime) maintainLongGrid(ctx context.Context) error {
	failed := false
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
		var book *common.VenueBook
		if r.factory != nil {
			var sourceErr error
			book, sourceErr = r.book(op, r.cfg.Venue.Profiles[0].Symbol)
			// With no valid source, only the fresh venue-mark stop may reduce risk.
			if sourceErr != nil {
				book = nil
			}
		}
		_, err := r.evaluateLongGrid(op, book, false)
		cancel()
		if err != nil && !failed && ctx.Err() == nil {
			if err := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("symbol", r.cfg.Venue.Profiles[0].Symbol), slog.String("reason", safeReason(err))); err != nil {
				return err
			}
		}
		failed = err != nil
		if err := native.Wait(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runtime) cancelGridOrders(ctx context.Context, snapshot *common.VenueSnapshot) (bool, error) {
	client, ok := r.client.Load().Client.(gateway.QuoteClient)
	if !ok {
		return false, ErrInvalidConfig
	}
	pending := false
	prefix := native.InstanceClientPrefix(r.cfg.Venue.Profiles[0])
	for _, order := range snapshot.GetOrders() {
		if !native.Active(order.Status) {
			continue
		}
		pending = true
		if !strings.HasPrefix(order.ClientId, prefix) {
			continue
		}
		id, err := strconv.ParseUint(order.OrderId, 10, 64)
		if err != nil {
			return true, err
		}
		if err := client.CancelQuote(ctx, gateway.CancelRequest{
			Scope:   gridScope(r.cfg.Venue.Profiles[0]),
			OrderID: id,
		}); err != nil {
			return true, err
		}
	}
	return pending, nil
}
