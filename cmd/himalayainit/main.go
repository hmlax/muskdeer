// Command himalayainit explicitly migrates and enables reviewed SDK fund budgets.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate"
	himalayamigrate "github.com/hmlax/muskdeer/deer/migrate/himalaya"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func main() { os.Exit(command()) }

func command() int {
	enable := flag.Bool("enable", false, "explicitly enable reviewed development SDK budgets")
	schemaOnly := flag.Bool("schema-only", false, "migrate the SDK extension without changing fund budgets")
	flag.Parse()
	if *enable == *schemaOnly || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "himalayainit requires exactly one of -enable or -schema-only")
		return 2
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 30*time.Second)
	defer cancel()
	if err := run(ctx, *enable); err != nil {
		fmt.Fprintln(os.Stderr, "SDK initialization failed")
		return 1
	}
	if *schemaOnly {
		fmt.Println("SDK extension migrated; fund budgets and native funds/orders unchanged")
	} else {
		fmt.Println("SDK extension and reviewed development budgets initialized; no native funds or orders changed")
	}
	return 0
}

func run(ctx context.Context, enable bool) (result error) {
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil || cfg.GetBusiness().GetExecutionVenue() != native.Venue || native.ValidateConfig(cfg) != nil {
		return native.ErrInvalid
	}
	database, err := postgres.New(ctx, cfg.Postgres, "himalaya-initialize")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, database.Close()) }()
	if err := migrate.CheckCurrent(ctx, database.GORM()); err != nil {
		return err
	}
	if err := himalayamigrate.Apply(ctx, database.GORM()); err != nil {
		return err
	}
	if !enable {
		return nil
	}
	store, err := himalaya.NewStore(database.GORM())
	if err != nil {
		return err
	}
	for _, profile := range cfg.Business.Himalaya.Profiles {
		if err := store.InitializeBudget(ctx, profile, true); err != nil {
			return err
		}
	}
	return nil
}
