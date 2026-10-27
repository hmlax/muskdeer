// Command marketprobe records bounded public market observations and deliberate
// same-provider reconnection. It has no database, account or trading capability.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketfeed"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

const (
	phaseObservation = "OBSERVATION"
	phaseResult      = "RESULT"
	messageUsage     = "marketprobe requires -source binance|okx, -symbol, duration 5s..2m and reconnect-after 1s..30s shorter than half the duration"
)

type evidence struct {
	Phase            string              `json:"phase"`
	RecordedAt       string              `json:"recorded_at"`
	Source           domain.MarketSource `json:"source"`
	Symbol           string              `json:"symbol"`
	Connection       int64               `json:"connection"`
	State            marketbook.State    `json:"state"`
	Reason           marketfeed.Reason   `json:"reason"`
	Generation       int64               `json:"generation"`
	Sequence         int64               `json:"sequence"`
	EventTime        string              `json:"event_time"`
	ReceivedAt       string              `json:"received_at"`
	Bid              string              `json:"bid"`
	Ask              string              `json:"ask"`
	Mid              string              `json:"mid"`
	ReadyConnections int                 `json:"ready_connections"`
	Passed           bool                `json:"passed"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("marketprobe", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	sourceName := flags.String("source", "", "explicit public source binance or okx")
	symbol := flags.String("symbol", "", "exact external spot symbol")
	duration := flags.Duration("duration", 30*time.Second, "total probe deadline")
	reconnect := flags.Duration("reconnect-after", 5*time.Second, "finite socket lifetime to prove rebuilding")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	source, err := domain.ParseMarketSource(*sourceName)
	if err != nil || source.ValidateSelection() != nil || *symbol == "" || flags.NArg() != 0 || *duration < 5*time.Second || *duration > 2*time.Minute || *reconnect < time.Second || *reconnect > 30*time.Second || *reconnect >= *duration/2 {
		_, _ = io.WriteString(diagnostic, messageUsage+"\n")
		return 2
	}
	config := marketfeed.DefaultConfig(source, marketwire.Instrument{
		ID:     1,
		Symbol: *symbol,
	})
	config.SessionLifetime = *reconnect
	feed, err := marketfeed.New(config)
	if err != nil {
		_, _ = io.WriteString(diagnostic, marketfeed.MessageConfig+"\n")
		return 2
	}
	call, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()
	encoder := json.NewEncoder(output)
	counts := make(map[int64]int)
	readyConnections := 0
	err = feed.Run(call, func(_ context.Context, observation marketfeed.Observation) error {
		if observation.State == marketbook.StateReady {
			counts[observation.Connection]++
			if counts[observation.Connection] > 2 {
				return nil
			}
			if counts[observation.Connection] == 2 {
				readyConnections++
			}
		}
		record := evidence{
			Phase:            phaseObservation,
			RecordedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			Source:           source,
			Symbol:           *symbol,
			Connection:       observation.Connection,
			State:            observation.State,
			Reason:           observation.Reason,
			ReadyConnections: readyConnections,
		}
		if observation.State == marketbook.StateReady {
			record.Generation = observation.View.Generation
			record.Sequence = observation.View.Sequence
			record.EventTime = observation.View.EventTime.Format(time.RFC3339Nano)
			record.ReceivedAt = observation.View.ReceivedAt.Format(time.RFC3339Nano)
			record.Bid = observation.View.Bid.Price.String()
			record.Ask = observation.View.Ask.Price.String()
			record.Mid = observation.View.Mid.String()
		}
		if err := encoder.Encode(record); err != nil {
			return err
		}
		if readyConnections >= 2 {
			cancel()
		}
		return nil
	})
	passed := readyConnections >= 2 && errors.Is(err, context.Canceled)
	if err := encoder.Encode(evidence{
		Phase:            phaseResult,
		RecordedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Source:           source,
		Symbol:           *symbol,
		ReadyConnections: readyConnections,
		Passed:           passed,
	}); err != nil {
		return 1
	}
	if !passed {
		return 1
	}
	return 0
}
