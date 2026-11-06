// Command coreinit explicitly prepares unfunded, simulator-only local core resources.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/corelocal"
	"github.com/hmlax/muskdeer/deer/migrate"
	policymigrate "github.com/hmlax/muskdeer/deer/migrate/runtimepolicy"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/processfailure"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := run(ctx)
	cancel()
	if err != nil {
		os.Exit(processfailure.Report(os.Stderr, "explicit local core preparation failed", err, 1))
	}
}

func run(ctx context.Context) (result error) {
	path := flag.String("config", "", "local execution service configuration")
	flag.Parse()
	if flag.NArg() != 0 {
		return corelocal.ErrResources
	}
	var cfg *configv1.ServiceConfig
	var err error
	if *path == "" {
		cfg, err = deer.LoadNacosConfig(ctx, "execution")
	} else {
		cfg, err = deer.LoadConfig(ctx, *path)
	}
	if err != nil {
		return err
	}
	if cfg.Name != "execution" || cfg.GetPostgres().GetDatabase() != "muskdeer_local" ||
		cfg.GetPostgres().GetSchema() != "public" || cfg.VenueConnection != nil ||
		cfg.GetJetstream().GetUsername() != "muskdeer-local" {
		return corelocal.ErrResources
	}
	db, err := postgres.New(ctx, cfg.Postgres, "muskdeer-coreinit")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	if err := migrate.CheckCurrent(ctx, db.GORM()); err != nil {
		return err
	}
	if err := policymigrate.Apply(ctx, db.GORM()); err != nil {
		return err
	}
	existing, err := runtimeconfig.LoadDatabasePolicy(ctx, db.GORM(), cfg)
	if err != nil && !errors.Is(err, runtimeconfig.ErrPolicyMissing) {
		return err
	}
	if existing != nil && existing.Business.ExecutionVenue != "" && existing.Business.ExecutionVenue != domain.ExecutionVenueSimulator {
		return corelocal.ErrResources
	}
	if err := corelocal.PrepareBroker(ctx, cfg.Jetstream, "portfolio_local"); err != nil {
		return err
	}
	resources, err := corelocal.Seed(ctx, db.GORM())
	if err != nil {
		return err
	}
	policies, err := corelocal.Policies(resources)
	if err != nil {
		return err
	}
	if err := runtimeconfig.SeedDatabasePolicies(ctx, db.GORM(), policies); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(resources)
}
