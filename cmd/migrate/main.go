// Command migrate explicitly applies embedded, checksummed schema upgrades.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/internal/migrate"
)

type result struct {
	Status  string `json:"status"`
	Version int64  `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

func main() {
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(signals, 30*time.Second)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "local service configuration with PostgreSQL")
	apply := flags.Bool("apply", false, "apply all pending embedded migrations")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return 0
		}
		return failure(diagnostic, "invalid migration arguments", 2)
	}
	if !*apply || *path == "" || flags.NArg() != 0 {
		return failure(diagnostic, "migrate requires -apply and -config with no positional arguments", 2)
	}
	if runtime.Version() != "go1.27.3" {
		return failure(diagnostic, "Go 1.27.3 is required", 1)
	}
	cfg, err := deer.LoadConfig(ctx, *path)
	if err != nil || cfg.GetPostgres() == nil {
		return failure(diagnostic, "migration requires valid PostgreSQL configuration", 1)
	}
	database, err := postgres.New(ctx, cfg.Postgres, "muskdeer-migrate")
	if err != nil {
		return failure(diagnostic, "migration database connection failed", 1)
	}
	version, applyErr := migrate.Apply(ctx, database.GORM())
	if err := errors.Join(applyErr, database.Close()); err != nil {
		if errors.Is(err, migrate.ErrHistoryMismatch) {
			return failure(diagnostic, migrate.ErrHistoryMismatch.Error(), 1)
		}
		return failure(diagnostic, "schema migration failed", 1)
	}
	if err := json.NewEncoder(output).Encode(result{
		Status:  "applied",
		Version: version,
	}); err != nil {
		return 1
	}
	return 0
}

func failure(output io.Writer, message string, code int) int {
	if err := json.NewEncoder(output).Encode(result{
		Status:  "failed",
		Message: message,
	}); err != nil {
		return 1
	}
	return code
}
