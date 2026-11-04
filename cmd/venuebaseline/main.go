// Command venuebaseline applies explicit development ledger decisions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate"
	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	himalaya "github.com/hmlax/muskdeer/execution/adapters/himalaya"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

const (
	messageManifestRequired = "venuebaseline requires -manifest"
	messageBaselineFailed   = "development baseline failed: "
	reasonDependency        = "dependency or approval failure"
	reasonRiskLimit         = "retained risk exceeds existing policy"
	reasonUnknown           = "unapproved trading obligation exists"
	reasonLease             = "execution ownership is not current"
	messageBaselineResult   = "%s: legacy_orders=%d active_orders=0 risk_retained=true applied=%t\n"
)

type baselineRunner struct {
	db     *gorm.DB
	store  *venueexecution.Store
	client *himalaya.Client
	leases domain.LeaseRepository
	owner  string
	cfg    *commonv1.VenuePolicy
	apply  bool
}

func main() { os.Exit(command()) }

func command() int {
	manifest := flag.String("manifest", "", "reviewed development baseline manifest")
	report := flag.String("report", "", "private report file")
	apply := flag.Bool("apply", false, "apply the explicit approval; default is read-only assessment")
	historical := flag.Bool("historical-intents", false, "use an explicit historical intent approval manifest")
	flag.Parse()
	if *manifest == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, messageManifestRequired)
		return 2
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 2*time.Minute)
	defer cancel()
	if err := run(ctx, *manifest, *report, *apply, *historical); err != nil {
		reason := reasonDependency
		if errors.Is(err, venueexecution.ErrRiskLimit) {
			reason = reasonRiskLimit
		}
		if errors.Is(err, domain.ErrOutcomeUnknown) {
			reason = reasonUnknown
		}
		if errors.Is(err, domain.ErrLeaseLost) {
			reason = reasonLease
		}
		fmt.Fprintln(os.Stderr, messageBaselineFailed+reason)
		return 1
	}
	return 0
}

func run(ctx context.Context, manifest, report string, apply, historical bool) (result error) {
	bytes, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	if len(bytes) > 1024*1024 {
		return venueexecution.ErrInvalidSubmit
	}
	batch := &commonv1.DevelopmentBaselineBatch{}
	history := &commonv1.HistoricalIntentApprovalBatch{}
	if historical {
		if err := protojson.Unmarshal(bytes, history); err != nil {
			return err
		}
		if len(history.Requests) == 0 || len(history.Requests) > 8 {
			return venueexecution.ErrInvalidSubmit
		}
	} else {
		if err := protojson.Unmarshal(bytes, batch); err != nil {
			return err
		}
		if len(batch.Requests) == 0 || len(batch.Requests) > 8 {
			return venueexecution.ErrInvalidSubmit
		}
	}
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil || cfg.GetBusiness().GetExecutionVenue() != native.Venue || native.ValidateConfig(cfg) != nil {
		return native.ErrInvalid
	}
	db, err := postgres.New(ctx, cfg.Postgres, "development-baseline")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	if err := migrate.CheckCurrent(ctx, db.GORM()); err != nil {
		return err
	}
	if err := venuemigrate.Check(ctx, db.GORM()); err != nil {
		return err
	}
	settings, err := venueconfig.LoadRuntime(ctx, db.GORM(), cfg.Business)
	if err != nil {
		return err
	}
	store, err := venueexecution.NewStore(db.GORM(), himalaya.Factory{})
	if err != nil {
		return err
	}
	client, err := himalaya.NewClient(ctx, venueexecution.Config{
		Target:  cfg.Business.Venue.Target,
		Token:   os.Getenv(cfg.Business.Venue.TokenEnv),
		Timeout: cfg.Business.OperationTimeout.AsDuration(),
	}, nil)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, client.Close(context.WithoutCancel(ctx))) }()
	leases, err := repository.NewLeases(db.GORM())
	if err != nil {
		return err
	}
	owner, err := ownerid.New(ctx, "development-baseline")
	if err != nil {
		return err
	}
	rows := make([]*executionmodelv1.ExecutionRecoveryBaseline, 0, len(batch.Requests))
	runner := baselineRunner{
		db:     db.GORM(),
		store:  store,
		client: client,
		leases: leases,
		owner:  owner,
		cfg:    settings.Venue,
		apply:  apply,
	}
	if historical {
		return runner.historical(ctx, history, report)
	}
	for _, request := range batch.Requests {
		row, err := runner.assess(ctx, request)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		fmt.Printf(messageBaselineResult, request.Scope.Symbol, len(request.OriginalOrders), apply)
	}

	if report != "" {
		encoded, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(report, encoded, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (r *baselineRunner) assess(ctx context.Context, request *commonv1.DevelopmentBaselineRequest) (*executionmodelv1.ExecutionRecoveryBaseline, error) {
	profile, err := native.Profile(r.cfg, request.GetScope())
	if err != nil {
		return nil, err
	}
	var lease domain.LeaseToken
	if r.apply {
		lease, err = r.acquire(ctx, request.Scope)
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := stableSnapshot(ctx, r.client, venueexecution.Scope{
		UserID: request.Scope.UserId,
		FundID: request.Scope.FundId,
		Symbol: request.Scope.Symbol,
	})
	if err != nil {
		return nil, err
	}
	if snapshot.Market.PriceTick != profile.PriceTick || snapshot.Market.QuantityStep != profile.QuantityStep {
		return nil, venueexecution.ErrInvalidSnapshot
	}
	row, err := r.store.AssessDevelopmentBaseline(ctx, request, snapshot)
	if err != nil {
		return nil, err
	}
	if r.apply {
		if err := r.store.ApplyDevelopmentBaseline(ctx, lease, request, snapshot); err != nil {
			return nil, err
		}
		if err := r.db.WithContext(ctx).Model(&executionmodelv1.ExecutionRecoveryBaseline{}).Where("account_id = ?", row.AccountId).First(row).Error; err != nil {
			return nil, err
		}
	}
	return row, nil
}

func (r *baselineRunner) acquire(ctx context.Context, scope *commonv1.VenueScope) (domain.LeaseToken, error) {
	for ctx.Err() == nil {
		token, err := r.leases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: native.ScopeKey(scope.UserId, scope.FundId),
		}, r.owner, r.cfg.LeaseTtl.AsDuration())
		if !errors.Is(err, domain.ErrLeaseHeld) {
			return token, err
		}
		if err := native.Wait(ctx, 200*time.Millisecond); err != nil {
			return domain.LeaseToken{}, err
		}
	}
	return domain.LeaseToken{}, ctx.Err()
}

func stableSnapshot(ctx context.Context, client *himalaya.Client, scope venueexecution.Scope) (*commonv1.VenueSnapshot, error) {
	read := func() (*commonv1.VenueSnapshot, error) {
		raw, err := client.Read(ctx, scope)
		if err != nil {
			return nil, err
		}
		return raw.Snapshot, nil
	}
	before, err := read()
	if err != nil {
		return nil, err
	}
	after, err := read()
	if err != nil {
		return nil, err
	}
	if len(before.Orders) != 0 || len(after.Orders) != 0 || len(before.Positions) != len(after.Positions) {
		return nil, venueexecution.ErrInvalidSnapshot
	}
	for i, position := range before.Positions {
		if !proto.Equal(position, after.Positions[i]) {
			return nil, venueexecution.ErrInvalidSnapshot
		}
	}
	first, e1 := native.Value(before.Available, false, true)
	second, e2 := native.Value(after.Available, false, true)
	if e1 != nil || e2 != nil || !first.Equal(second) {
		return nil, venueexecution.ErrInvalidSnapshot
	}
	return after, nil
}
