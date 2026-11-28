package native

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

type makerProposal struct {
	sequence int64
	trigger  string
	at       time.Time
}

type makerHarness struct {
	mu           sync.Mutex
	active       bool
	observeErr   error
	blockObserve chan struct{}
	observed     int
	polled       int
	pollBook     *common.VenueBook
	proposals    []makerProposal
	reports      []string
}

func (h *makerHarness) loop(interval time.Duration) *makerLoop {
	return &makerLoop{
		interval: interval,
		timeout:  time.Second,
		observe: func(ctx context.Context) (*makerState, error) {
			h.mu.Lock()
			block := h.blockObserve
			h.mu.Unlock()
			if block != nil {
				select {
				case <-block:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			h.observed++
			if h.observeErr != nil {
				return nil, h.observeErr
			}
			return &makerState{active: h.active, profile: &common.VenueProfilePolicy{Symbol: "BTCUSDT"}, usableUntil: time.Now().Add(interval + time.Second)}, nil
		},
		book: func(context.Context) (*common.VenueBook, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.polled++
			return h.pollBook, nil
		},
		propose: func(_ context.Context, _ *makerState, book *common.VenueBook, trigger string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.proposals = append(h.proposals, makerProposal{sequence: book.GetSequence(), trigger: trigger, at: time.Now()})
			return nil
		},
		report: func(_ context.Context, step string, _ error) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.reports = append(h.reports, step)
			return nil
		},
	}
}

func (h *makerHarness) snapshot() (proposals []makerProposal, polled int, reports []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]makerProposal(nil), h.proposals...), h.polled, append([]string(nil), h.reports...)
}

func freshBook(sequence int64) *common.VenueBook {
	return &common.VenueBook{Symbol: "BTCUSDT", Sequence: sequence, ValidUntilMs: time.Now().Add(1500 * time.Millisecond).UnixMilli()}
}

func startMakerLoop(t *testing.T, h *makerHarness, interval time.Duration) (chan *common.VenueBook, func() error) {
	books := make(chan *common.VenueBook, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- h.loop(interval).run(ctx, books) }()
	return books, func() error { cancel(); return <-done }
}

func TestDirectMakerProposesOnBookWithoutWaitingForPoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &makerHarness{active: true}
		books, stop := startMakerLoop(t, h, 200*time.Millisecond)
		synctest.Wait()
		start := time.Now()
		books <- freshBook(10)
		synctest.Wait()
		proposals, polled, _ := h.snapshot()
		if len(proposals) != 1 || proposals[0].sequence != 10 || proposals[0].trigger != makerTriggerBook || !proposals[0].at.Equal(start) || polled != 0 {
			t.Fatalf("proposals=%+v polled=%d", proposals, polled)
		}
		// A newer frame is proposed at once; an older, duplicate or expired one is not.
		books <- freshBook(10)
		synctest.Wait()
		books <- freshBook(9)
		synctest.Wait()
		expired := freshBook(20)
		expired.ValidUntilMs = time.Now().UnixMilli()
		books <- expired
		synctest.Wait()
		books <- freshBook(11)
		synctest.Wait()
		proposals, polled, _ = h.snapshot()
		if len(proposals) != 2 || proposals[1].sequence != 11 || polled != 0 {
			t.Fatalf("proposals=%+v polled=%d", proposals, polled)
		}
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestDirectMakerFallsBackToPollingWhenFramesStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &makerHarness{active: true, pollBook: freshBook(5)}
		books, stop := startMakerLoop(t, h, 200*time.Millisecond)
		start := time.Now()
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		proposals, polled, _ := h.snapshot()
		if len(proposals) != 1 || proposals[0].trigger != makerTriggerPoll || proposals[0].sequence != 5 || polled != 1 || proposals[0].at.Sub(start) != 200*time.Millisecond {
			t.Fatalf("proposals=%+v polled=%d", proposals, polled)
		}
		// A frame resets the fallback: no poll until a full quiet interval passes.
		time.Sleep(150 * time.Millisecond)
		books <- freshBook(6)
		synctest.Wait()
		time.Sleep(150 * time.Millisecond)
		synctest.Wait()
		if _, polled, _ = h.snapshot(); polled != 1 {
			t.Fatalf("polled=%d", polled)
		}
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		proposals, polled, _ = h.snapshot()
		if polled != 2 || len(proposals) != 3 || proposals[2].trigger != makerTriggerPoll {
			t.Fatalf("proposals=%+v polled=%d", proposals, polled)
		}
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestDirectMakerDoesNotProposeWithoutUsableControlState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &makerHarness{active: false, pollBook: freshBook(1)}
		books, stop := startMakerLoop(t, h, 200*time.Millisecond)
		synctest.Wait()
		books <- freshBook(2)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if proposals, polled, _ := h.snapshot(); len(proposals) != 0 || polled != 0 {
			t.Fatalf("stopped instance proposed: %+v polled=%d", proposals, polled)
		}
		// A failed control read withdraws the previous observation.
		h.mu.Lock()
		h.active = true
		h.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		books <- freshBook(3)
		synctest.Wait()
		h.mu.Lock()
		h.observeErr = errors.New("database unavailable")
		h.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		books <- freshBook(4)
		synctest.Wait()
		// The fallback poll may share an instant with the failing refresh; only
		// the frame after the withdrawn observation is asserted here.
		proposals, _, reports := h.snapshot()
		var events []int64
		for _, proposal := range proposals {
			if proposal.trigger == makerTriggerBook {
				events = append(events, proposal.sequence)
			}
		}
		if len(events) != 1 || events[0] != 3 || len(reports) != 1 || reports[0] != "observe" {
			t.Fatalf("proposals=%+v reports=%v", proposals, reports)
		}
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestDirectMakerStateExpiresWhenRefreshStalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &makerHarness{active: true, pollBook: freshBook(0)}
		books, stop := startMakerLoop(t, h, 200*time.Millisecond)
		synctest.Wait()
		h.mu.Lock()
		h.blockObserve = make(chan struct{})
		h.mu.Unlock()
		// The stalled read began at 200ms; the previous observation is usable
		// until interval+timeout after its own start, then proposals stop.
		time.Sleep(1100 * time.Millisecond)
		books <- freshBook(1)
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		books <- freshBook(2)
		synctest.Wait()
		proposals, _, _ := h.snapshot()
		var events []int64
		for _, proposal := range proposals {
			if proposal.trigger == makerTriggerBook {
				events = append(events, proposal.sequence)
			}
		}
		if len(events) != 1 || events[0] != 1 {
			t.Fatalf("proposals=%+v", proposals)
		}
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestOfferLatestBookReplacesPendingFrame(t *testing.T) {
	books := make(chan *common.VenueBook, 1)
	offerLatestBook(books, &common.VenueBook{Sequence: 1})
	offerLatestBook(books, &common.VenueBook{Sequence: 2})
	if got := (<-books).GetSequence(); got != 2 {
		t.Fatalf("sequence=%d", got)
	}
}

func TestDirectMakerWorkerSelection(t *testing.T) {
	maker := &venueconfig.RuntimeConfig{InstanceID: 1, Template: domain.StrategyTemplateMarketMaker}
	if !directMakerWorker(maker) {
		t.Fatal("instance maker must be event driven")
	}
	for _, cfg := range []*venueconfig.RuntimeConfig{
		{InstanceID: 0, Template: domain.StrategyTemplateMarketMaker},
		{InstanceID: 1, Template: domain.StrategyTemplateGrid},
	} {
		if directMakerWorker(cfg) {
			t.Fatalf("polling path changed: %+v", cfg)
		}
	}
}
