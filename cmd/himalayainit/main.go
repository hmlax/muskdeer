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

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate"
	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	himalaya "github.com/hmlax/muskdeer/execution/adapters/himalaya"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

const (
	messageInvalidProfileImport = "invalid database profile import"
	messageProfilesImported     = "Database business profiles imported; existing enablement and financial observations retained"
)

func main() { os.Exit(command()) }

func command() int {
	enable := flag.Bool("enable", false, "explicitly enable reviewed development SDK budgets")
	schemaOnly := flag.Bool("schema-only", false, "migrate the SDK extension without changing fund budgets")
	profilesFile := flag.String("profiles", "", "explicitly import reviewed database business profiles while services are stopped")
	flag.Parse()
	modes := 0
	for _, selected := range []bool{*enable, *schemaOnly, *profilesFile != ""} {
		if selected {
			modes++
		}
	}
	if modes != 1 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "himalayainit requires exactly one of -enable, -schema-only or -profiles FILE")
		return 2
	}
	var profiles *commonv1.HimalayaProfileImport
	if *profilesFile != "" {
		var err error
		profiles, err = readProfiles(*profilesFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, messageInvalidProfileImport)
			return 2
		}
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 30*time.Second)
	defer cancel()
	if err := run(ctx, *enable, profiles); err != nil {
		fmt.Fprintln(os.Stderr, "SDK initialization failed")
		return 1
	}
	switch {
	case *schemaOnly:
		fmt.Println("SDK extension migrated; fund budgets and native funds/orders unchanged")
	case profiles != nil:
		fmt.Println(messageProfilesImported)
	default:
		fmt.Println("SDK extension and reviewed development budgets initialized; no native funds or orders changed")
	}
	return 0
}

func run(ctx context.Context, enable bool, profiles *commonv1.HimalayaProfileImport) (result error) {
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
	if err := venuemigrate.Apply(ctx, database.GORM()); err != nil {
		return err
	}
	if !enable && profiles == nil {
		return nil
	}
	store, err := venueexecution.NewStore(database.GORM(), himalaya.Factory{})
	if err != nil {
		return err
	}
	if profiles != nil {
		return store.ImportProfiles(ctx, profiles)
	}
	settings, err := venueconfig.LoadRuntime(ctx, database.GORM(), cfg.Business)
	if err != nil {
		return err
	}
	for _, profile := range settings.Himalaya.Profiles {
		if err := store.InitializeBudget(ctx, profile, true); err != nil {
			return err
		}
	}
	return nil
}

func readProfiles(path string) (*commonv1.HimalayaProfileImport, error) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1024*1024 {
		return nil, native.ErrInvalid
	}
	var input commonv1.HimalayaProfileImport
	if protojson.Unmarshal(data, &input) != nil || len(input.Profiles) == 0 || len(input.Profiles) > 8 {
		return nil, native.ErrInvalid
	}
	return &input, nil
}
